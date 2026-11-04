// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/secrets"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// enterpriseConn is the service's connection to the organization's
// service (spec_053_enterprise §3): one client for the address in the
// settings, registered so the models renew through it too, and one
// sign-in at a time.
type enterpriseConn struct {
	mu        sync.Mutex
	c         *enterprise.Client
	signingIn sync.Mutex
	// stop ends the catalog's loop for c; kick asks it to fetch now (a
	// sign-in, a connection); done is closed when it has ended.
	stop func()
	kick chan struct{}
	done chan struct{}
	// retry is how long the loop waits after the stream fails, before
	// asking again; idle, how often it looks for a sign-in (tests shorten
	// them).
	retry, idle time.Duration
	// gitHelper replaces the credential helper Git runs (tests).
	gitHelper string
	// syncs are the organization workspaces kept in step; syncEvery how
	// often it looks for clones to keep, syncQuiet and syncPoll the
	// syncers' (tests shorten them).
	syncs                          orgSyncs
	syncEvery, syncQuiet, syncPoll time.Duration
	// HTTP is what the calls go through (tests set it); nil for the
	// default client.
	http *http.Client
	// orgFor is each folder's last answer from OrgWorkspaceFor, kept for
	// when the service doesn't answer (a Viewer's clone stays read-only).
	orgForMu sync.Mutex
	orgFor   map[string][2]string
}

// discoverTimeout bounds checking an address.
const discoverTimeout = 15 * time.Second

// enterpriseClient is the client for the settings' address, nil when
// there's none.
func (s *Server) enterpriseClient() (*enterprise.Client, error) {
	cfg, err := config.Load(s.configDir)
	if err != nil {
		return nil, err
	}
	return s.enterpriseFor(cfg.Enterprise.URL)
}

func (s *Server) enterpriseFor(raw string) (*enterprise.Client, error) {
	if raw == "" {
		return nil, nil
	}
	u, err := enterprise.Normalize(raw)
	if err != nil {
		return nil, err
	}
	s.ent.mu.Lock()
	defer s.ent.mu.Unlock()
	if s.ent.c == nil || s.ent.c.URL() != u {
		s.ent.stopCatalog()
		s.ent.c = enterprise.New(u, secrets.Default(s.configDir), enterprise.Options{HTTP: s.ent.http, Client: "blitz " + s.version,
			BrandingDir: config.EnterpriseDir(s.configDir, u)})
		enterprise.Register(s.ent.c)
		s.startCatalog(s.ent.c)
	}
	return s.ent.c, nil
}

// startCatalog keeps the organization's catalog current for c while it's
// the connection (spec_053_enterprise EN-32): fetched once signed in,
// again on each version its service's stream announces, and on the
// policy's refresh while the stream is down; open workspaces load each
// change. Called with s.ent.mu held.
func (s *Server) startCatalog(c *enterprise.Client) {
	ctx, cancel := context.WithCancel(context.Background())
	kick, done := make(chan struct{}, 1), make(chan struct{})
	s.ent.stop, s.ent.kick, s.ent.done = cancel, kick, done
	retry, idle := s.ent.retry, s.ent.idle
	if retry == 0 {
		retry = 30 * time.Second
	}
	if idle == 0 {
		idle = time.Minute
	}
	dir := s.brandingDir(c)
	syncKick := make(chan struct{}, 1)
	s.ent.syncs.kick = syncKick
	var loops sync.WaitGroup
	loops.Go(func() { s.runSyncs(ctx, c, syncKick) })
	go func() {
		defer close(done)
		defer loops.Wait()
		wait := func(d time.Duration) bool {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return false
			case <-kick:
			case <-t.C:
			}
			return true
		}
		refresh := func() time.Duration {
			m, changed, err := c.RefreshCatalog(ctx, dir)
			if err != nil && ctx.Err() == nil {
				slog.Warn("the organization's catalog", "error", err)
			}
			if changed {
				configService{s}.changed(ctx, "", "")
			}
			every := 15 * time.Minute
			if m != nil {
				if d, err := time.ParseDuration(m.Policy.Refresh); err == nil && d > 0 {
					every = d
				}
			}
			return every
		}
		for ctx.Err() == nil {
			if !c.Status().SignedIn {
				if !wait(idle) {
					return
				}
				continue
			}
			every := refresh()
			// Each version the stream announces is fetched as it comes;
			// a stream that ends or can't start is tried again later.
			if err := c.WatchCatalog(ctx, func() { refresh() }); err != nil && ctx.Err() == nil {
				if !wait(min(retry, every)) {
					return
				}
			}
		}
	}()
}

// stopCatalog ends the catalog's loop, if one runs, and waits for it.
// Called with mu held.
func (e *enterpriseConn) stopCatalog() {
	if e.stop == nil {
		return
	}
	e.stop()
	<-e.done
	e.stop = nil
}

// kickCatalog asks the catalog's loop to fetch now, and the syncs to look
// again for clones to keep in step.
func (e *enterpriseConn) kickCatalog() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, k := range []chan struct{}{e.kick, e.syncs.kick} {
		if k != nil {
			select {
			case k <- struct{}{}:
			default:
			}
		}
	}
}

// enterpriseState is c's state; with check, after asking the service.
func (s *Server) enterpriseState(ctx context.Context, c *enterprise.Client, check bool) *pb.EnterpriseState {
	if c == nil {
		return &pb.EnterpriseState{}
	}
	st := c.Status()
	out := &pb.EnterpriseState{Url: st.URL, SignedIn: st.SignedIn, TermsVersion: st.Terms, Managed: config.ManagedURL() != ""}
	info := st.Info
	if check {
		cctx, cancel := context.WithTimeout(ctx, discoverTimeout)
		defer cancel()
		out.Checked = true
		fresh, err := enterprise.Discover(cctx, s.ent.httpClient(), st.URL)
		if err != nil {
			out.Error = err.Error()
		} else {
			out.Reachable, info = true, fresh
		}
	}
	out.Name, out.Version, out.Capabilities = info.Name, info.Version, info.Capabilities
	if st.SignedIn {
		out.Email, out.PersonName, out.Roles = st.Person.Email, st.Person.Name, st.Person.Roles
		if !st.Renews.IsZero() {
			out.RenewsUnix = st.Renews.Unix()
		}
	}
	return out
}

func (e *enterpriseConn) httpClient() *http.Client {
	if e.http != nil {
		return e.http
	}
	return enterprise.HTTPClient()
}

func (h configService) GetEnterprise(ctx context.Context, r req[pb.GetEnterpriseRequest]) (*connect.Response[pb.GetEnterpriseResponse], error) {
	c, err := h.s.enterpriseClient()
	if err != nil {
		return nil, invalid(err)
	}
	return ok(&pb.GetEnterpriseResponse{State: h.s.enterpriseState(ctx, c, r.Msg.Check)})
}

func (h configService) ConnectEnterprise(ctx context.Context, r req[pb.ConnectEnterpriseRequest]) (*connect.Response[pb.ConnectEnterpriseResponse], error) {
	if config.ManagedURL() != "" {
		return nil, apiError(connect.CodeFailedPrecondition, "MANAGED", config.ErrManaged)
	}
	previous, _ := h.s.enterpriseClient()
	var next *enterprise.Client
	if r.Msg.Url != "" {
		u, err := enterprise.Normalize(r.Msg.Url)
		if err != nil {
			return nil, apiError(connect.CodeInvalidArgument, "INVALID_ADDRESS", err)
		}
		cctx, cancel := context.WithTimeout(ctx, discoverTimeout)
		defer cancel()
		if _, err := enterprise.Discover(cctx, h.s.ent.httpClient(), u); err != nil {
			return nil, apiError(connect.CodeUnavailable, "UNREACHABLE", err)
		}
		r.Msg.Url = u
	}
	if previous != nil && previous.URL() != r.Msg.Url {
		// Another organization, or none: this one's session ends.
		if err := previous.SignOut(ctx); err != nil {
			slog.Warn("signing out of the organization's service", "error", err)
		}
		// And how it looked: the app is Blitz again (EN-54).
		i18n.SetAppName("")
		if err := enterprise.ForgetBranding(h.s.brandingDir(previous)); err != nil {
			slog.Warn("removing the organization's branding", "error", err)
		}
	}
	path, err := config.SetEnterpriseURL(h.s.configDir, r.Msg.Url)
	if err != nil {
		return nil, invalid(err)
	}
	if next, err = h.s.enterpriseFor(r.Msg.Url); err != nil {
		return nil, invalid(err)
	}
	h.changed(ctx, "", path)
	return ok(&pb.ConnectEnterpriseResponse{State: h.s.enterpriseState(ctx, next, r.Msg.Url != "")})
}

func (h configService) EnterpriseSignIn(ctx context.Context, r req[pb.EnterpriseSignInRequest], stream *connect.ServerStream[pb.EnterpriseSignInResponse]) error {
	c, err := h.s.enterpriseClient()
	if err != nil {
		return invalid(err)
	}
	if c == nil {
		return apiError(connect.CodeFailedPrecondition, "NOT_CONNECTED", errors.New("connect to your organization's service first"))
	}
	if !h.s.ent.signingIn.TryLock() {
		return apiError(connect.CodeAborted, "SIGN_IN_RUNNING", errors.New("a sign-in to your organization is already under way"))
	}
	defer h.s.ent.signingIn.Unlock()
	show := func(p enterprise.Prompt) { _ = stream.Send(&pb.EnterpriseSignInResponse{Url: p.URL, Code: p.Code}) }
	if r.Msg.Device {
		err = c.SignInDevice(ctx, show)
	} else {
		err = c.SignInBrowser(ctx, show, nil)
	}
	if err != nil {
		if ctx.Err() != nil {
			return connect.NewError(connect.CodeCanceled, ctx.Err())
		}
		return apiError(connect.CodeUnauthenticated, "SIGN_IN_FAILED", err)
	}
	h.changed(ctx, "", "")
	h.s.ent.kickCatalog() // the catalog, now there's a sign-in
	return stream.Send(&pb.EnterpriseSignInResponse{Done: true, State: h.s.enterpriseState(ctx, c, false)})
}

func (h configService) EnterpriseSignOut(ctx context.Context, _ req[pb.EnterpriseSignOutRequest]) (*connect.Response[pb.EnterpriseSignOutResponse], error) {
	c, err := h.s.enterpriseClient()
	if err != nil {
		return nil, invalid(err)
	}
	if c != nil {
		if err := c.SignOut(ctx); err != nil {
			// Signed out here all the same.
			slog.Warn("signing out of the organization's service", "error", err)
		}
		h.changed(ctx, "", "")
	}
	return ok(&pb.EnterpriseSignOutResponse{State: h.s.enterpriseState(ctx, c, false)})
}

// brandingDir is the organization's folder (spec_053_enterprise EN-54):
// ~/.blitz/enterprise/<the service's host>, so another's never mixes in.
func (s *Server) brandingDir(c *enterprise.Client) string {
	return config.EnterpriseDir(s.configDir, c.URL())
}

func brandingMsg(b *enterprise.Branding) *pb.Branding {
	if b == nil {
		return nil
	}
	out := &pb.Branding{Name: b.Name, ShortName: b.ShortName, Accent: b.Accent, AccentDark: b.AccentDark, Support: b.Support, About: b.About, Images: b.Images}
	for _, d := range b.Terms {
		out.Documents = append(out.Documents, &pb.OrgDocument{Document: d.Document, Version: d.Version, Text: d.Text, Url: d.URL})
	}
	return out
}

func (h configService) GetBranding(ctx context.Context, r req[pb.GetBrandingRequest]) (*connect.Response[pb.GetBrandingResponse], error) {
	c, err := h.s.enterpriseClient()
	if err != nil {
		return nil, invalid(err)
	}
	if c == nil {
		return ok(&pb.GetBrandingResponse{})
	}
	dir := h.s.brandingDir(c)
	if !r.Msg.Refresh {
		return ok(&pb.GetBrandingResponse{Branding: brandingMsg(enterprise.LoadBranding(dir))})
	}
	cctx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()
	b, err := c.RefreshBranding(cctx, dir)
	if b != nil {
		// The service's own words (notifications) say the organization's name too (EN-51).
		i18n.SetAppName(b.Name)
	}
	res := &pb.GetBrandingResponse{Branding: brandingMsg(b)}
	if err != nil {
		res.Error = err.Error()
	}
	return ok(res)
}

func (h configService) GetTerms(ctx context.Context, _ req[pb.GetTermsRequest]) (*connect.Response[pb.GetTermsResponse], error) {
	c, err := h.s.enterpriseClient()
	if err != nil {
		return nil, invalid(err)
	}
	if c == nil {
		return nil, apiError(connect.CodeFailedPrecondition, "NOT_CONNECTED", errors.New("connect to your organization's service first"))
	}
	t, err := c.Terms(ctx)
	if err != nil {
		return nil, enterpriseError(err)
	}
	return ok(&pb.GetTermsResponse{Version: t.Version, Accepted: t.Accepted, Text: t.Text, Url: t.URL})
}

func (h configService) AcceptTerms(ctx context.Context, r req[pb.AcceptTermsRequest]) (*connect.Response[pb.AcceptTermsResponse], error) {
	c, err := h.s.enterpriseClient()
	if err != nil {
		return nil, invalid(err)
	}
	if c == nil {
		return nil, apiError(connect.CodeFailedPrecondition, "NOT_CONNECTED", errors.New("connect to your organization's service first"))
	}
	if err := c.AcceptTerms(ctx, r.Msg.Version); err != nil {
		return nil, enterpriseError(err)
	}
	h.changed(ctx, "", "") // models refused for the terms can be built now
	return ok(&pb.AcceptTermsResponse{State: h.s.enterpriseState(ctx, c, false)})
}

// enterpriseError gives an error from the organization's service its
// code: signed out, a refusal by its status, else unavailable.
func enterpriseError(err error) error {
	var ref *enterprise.Refusal
	switch {
	case errors.Is(err, enterprise.ErrNotSignedIn):
		return apiError(connect.CodeUnauthenticated, "NOT_SIGNED_IN", err)
	case errors.As(err, &ref) && ref.Status == http.StatusConflict:
		return apiError(connect.CodeAborted, "CONFLICT", err)
	case errors.As(err, &ref) && ref.Status/100 == 4:
		return apiError(connect.CodeFailedPrecondition, strings.ToUpper(ref.Code), err)
	default:
		return apiError(connect.CodeUnavailable, "UNREACHABLE", err)
	}
}
