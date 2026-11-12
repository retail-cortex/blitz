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

package runtime

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/observability"
	"github.com/retail-cortex/blitz/pkg/redact"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/adk/v2/model/openaimodel"
	"google.golang.org/genai"
)

// The api provider (spec_015_models §3.4): the adapter for the API the
// model speaks behind the gateway (MDL-112), with a middleware that signs
// each request with the configured headers and the session's token, sends
// it to that API's path (MDL-113), and fixes up the gateway's answers
// (apiresponse.go).

// apiFormat is the API a model behind the gateway speaks, which its
// requests are written in (MDL-112).
type apiFormat int

const (
	// formatResponses is OpenAI's Responses API, through the openai adapter.
	formatResponses apiFormat = iota
	// formatMessages is Anthropic's Messages API, through the anthropic
	// adapter.
	formatMessages
	// formatChat is OpenAI's Chat Completions API, through the openai
	// adapter.
	formatChat
	// formatGemini is the Gemini API, through the gemini adapter.
	formatGemini
	// formatVertex is Vertex AI's API for Google's models, through the
	// gemini adapter.
	formatVertex
)

// apiFormats are the formats by [llm.api.apis]'s names.
var apiFormats = map[string]apiFormat{
	config.GatewayResponses:       formatResponses,
	config.GatewayMessages:        formatMessages,
	config.GatewayChatCompletions: formatChat,
	config.GatewayGemini:          formatGemini,
	config.GatewayVertex:          formatVertex,
}

// apiNames are [llm.api.apis]'s names by format.
var apiNames = func() map[apiFormat]string {
	out := map[apiFormat]string{}
	for name, f := range apiFormats {
		out[f] = name
	}
	return out
}()

// google reports whether f is one of Google's generateContent APIs.
func (f apiFormat) google() bool { return f == formatGemini || f == formatVertex }

// googleCallRE is a gemini adapter's model call's path: its API version,
// Vertex AI's publisher, the model and the method.
var googleCallRE = regexp.MustCompile(`/[^/]+/(?:publishers/google/)?models/([^/:]+):(generateContent|streamGenerateContent)$`)

// apiPlaceholderKey is the SDK's key: never sent (MDL-104), but set so the
// SDK doesn't take OPENAI_API_KEY from the environment.
const apiPlaceholderKey = "unused"

// adapterPaths are where each adapter sends a model call under the base
// URL it's given, which sign moves to the API's path (MDL-113); Google's
// match googleCallRE.
var adapterPaths = map[apiFormat]string{
	formatResponses: "/responses",
	formatMessages:  "/v1/messages",
	formatChat:      "/chat/completions",
}

// escapedPlaceholder is the placeholder as a URL's string writes it.
var escapedPlaceholder = url.PathEscape(config.TokenPlaceholder)

// openAIEnvHeaders are headers the SDK fills from the environment, removed
// unless configured, so no OpenAI setting reaches the gateway.
var openAIEnvHeaders = []string{"Authorization", "OpenAI-Organization", "OpenAI-Project"}

// anthropicAuthHeaders are the anthropic adapter's credentials (the
// placeholder key), removed unless configured.
var anthropicAuthHeaders = []string{"X-Api-Key", "Authorization"}

// googleAuthHeaders are the gemini adapter's (its placeholder key's).
var googleAuthHeaders = []string{"X-Goog-Api-Key", "Authorization"}

// apiProvider signs the api provider's requests.
type apiProvider struct {
	cfg    config.APIConfig
	format apiFormat
	model  string         // the model's name, the paths' {model}
	tokens *sessionTokens // nil when nothing uses the placeholder
	// source gives the token in place of a script: the organization's
	// service's sign-in (spec_053_enterprise EN-20), which renews itself.
	source     tokenSource
	query      url.Values // base_url's query, put on every request
	tokenInURL bool       // base_url uses the placeholder

	mu   sync.Mutex
	drop []string // request fields the model refuses (MDL-104b)
	// unstreamed is set once the gateway refuses to stream the Responses
	// API (MDL-100c): its requests are then sent whole.
	unstreamed bool
}

// newAPIModel builds the api provider's model from its settings. The token
// script isn't run here (MDL-103): a short-lived token isn't spent before a
// session needs it.
func newAPIModel(ctx context.Context, c config.APIConfig, name string, pol retryPolicy) (model.LLM, error) {
	p, err := newAPIProvider(c, apiFormats[c.APIOf(cmp.Or(name, c.Model))])
	if err != nil {
		return nil, err
	}
	return buildAPIModel(ctx, p, name, pol)
}

// newAPIProvider signs requests in format with c, which it checks first;
// the token script isn't run until a request needs it.
func newAPIProvider(c config.APIConfig, format apiFormat) (*apiProvider, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	p := &apiProvider{cfg: c, format: format}
	if c.UsesToken() {
		p.tokens = gatewayTokens(c, p.fetchToken)
	}
	return p, nil
}

// sharedTokens are the token caches by token script (its language and
// text): every model of a gateway, and its model list, use one, so a
// token still good isn't fetched again (MDL-103).
var sharedTokens = struct {
	sync.Mutex
	byScript map[string]*sessionTokens
}{byScript: map[string]*sessionTokens{}}

// gatewayTokens is the token cache for c's script, made with fetch the
// first time.
func gatewayTokens(c config.APIConfig, fetch func(context.Context) (string, error)) *sessionTokens {
	key := c.Language() + "\x00" + c.TokenScript
	sharedTokens.Lock()
	defer sharedTokens.Unlock()
	t, ok := sharedTokens.byScript[key]
	if !ok {
		t = newSessionTokens(fetch)
		sharedTokens.byScript[key] = t
	}
	return t
}

// buildAPIModel is the model p signs the requests of: name, else the
// settings' model.
func buildAPIModel(ctx context.Context, p *apiProvider, name string, pol retryPolicy) (model.LLM, error) {
	name = cmp.Or(name, p.cfg.Model)
	p.model = name
	if p.format == formatResponses && rejectsSampling(name) {
		p.drop = slices.Clone(samplingParams)
	}
	baseURL, err := p.baseURL()
	if err != nil {
		return nil, err
	}
	if log := gatewayLog(); log.Enabled(ctx, slog.LevelDebug) {
		log.DebugContext(ctx, "gateway model", "api", apiNames[p.format], "model", name,
			"base_url", redact.Mask(p.cfg.BaseURL), "path", p.cfg.PathOf(apiNames[p.format]),
			"headers", slices.Sorted(maps.Keys(p.cfg.Headers)), "token_script", p.cfg.UsesToken(), "organization", p.source != nil)
	}
	if p.format == formatMessages {
		// No credentials or base URL from the environment; no server-side
		// fallback, which needs Anthropic's own API.
		opts := append(pol.anthropicOptions(),
			anthropicoption.WithoutEnvironmentDefaults(),
			anthropicoption.WithAPIKey(apiPlaceholderKey),
			anthropicoption.WithBaseURL(baseURL),
			anthropicoption.WithMiddleware(p.middleware))
		inner := &anthropicModel{client: anthropic.NewClient(opts...), name: name, fallbacks: "off"}
		return &apiModel{inner: inner, p: p}, nil
	}
	if p.format.google() {
		return buildGoogleAPIModel(ctx, p, name, baseURL, pol)
	}
	opts := append(pol.openAIOptions(), option.WithMiddleware(p.middleware))
	api := openaimodel.APIResponses
	if p.format == formatChat {
		api = openaimodel.APIChatCompletions
	}
	inner, err := newOpenAIModelFor(ctx, name, apiPlaceholderKey, baseURL, api, opts...)
	if err != nil {
		return nil, err
	}
	return &apiModel{inner: inner, p: p}, nil
}

// baseURL is the base URL p's clients resolve their paths against, the
// placeholder escaped. The SDKs drop its query, which goes on each request
// instead (sign).
func (p *apiProvider) baseURL() (string, error) {
	p.tokenInURL = strings.Contains(p.cfg.BaseURL, config.TokenPlaceholder)
	base, rawQuery, _ := strings.Cut(p.cfg.BaseURL, "?")
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", fmt.Errorf("[llm.api] base_url's query: %w", err)
	}
	p.query = query
	return strings.ReplaceAll(base, config.TokenPlaceholder, escapedPlaceholder), nil
}

// buildGoogleAPIModel is a model speaking Gemini's or Vertex AI's API,
// through the gemini adapter with a placeholder key (Vertex AI's express
// mode: no project, no Google credentials) and p's middleware as its
// client's transport.
func buildGoogleAPIModel(ctx context.Context, p *apiProvider, name, baseURL string, pol retryPolicy) (model.LLM, error) {
	cc := pol.geminiConfig(apiPlaceholderKey)
	cc.Backend = genai.BackendGeminiAPI
	if p.format == formatVertex {
		cc.Backend = genai.BackendVertexAI
	}
	// Not from the environment (GOOGLE_CLOUD_PROJECT and the rest): the
	// gateway's model is the gateway's.
	cc.Project, cc.Location, cc.Credentials = "", "", nil
	cc.HTTPOptions.BaseURL = strings.TrimSuffix(baseURL, "/") + "/"
	inner := cc.HTTPClient
	cc.HTTPClient = &http.Client{Timeout: inner.Timeout, Transport: middlewareTransport{p.middleware, inner}}
	m, err := gemini.NewModel(ctx, name, cc)
	if err != nil {
		return nil, err
	}
	return &apiModel{inner: m, p: p}, nil
}

// middlewareTransport runs the gemini adapter's requests through mw, as
// the other adapters' clients run theirs.
type middlewareTransport struct {
	mw     func(*http.Request, option.MiddlewareNext) (*http.Response, error)
	client *http.Client
}

// RoundTrip sends req through mw, then client.
func (t middlewareTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.mw(req, t.client.Do)
}

// isModelCall reports whether req asks for a model response, in the
// model's API.
func (p *apiProvider) isModelCall(req *http.Request) bool {
	if req.Method != http.MethodPost {
		return false
	}
	path := strings.TrimSuffix(req.URL.Path, "/")
	if p.format.google() {
		return googleCallRE.MatchString(path)
	}
	return strings.HasSuffix(path, adapterPaths[p.format])
}

// routed is u with the adapter's path for a model call replaced by the
// API's (MDL-113): {model} the model's name as it is (a "/" in it is a
// path's), {method} Google's.
func (p *apiProvider) routed(u *url.URL) *url.URL {
	path := strings.TrimSuffix(u.Path, "/")
	var prefix, method string
	if p.format.google() {
		loc := googleCallRE.FindStringSubmatchIndex(path)
		prefix, method = path[:loc[0]], path[loc[4]:loc[5]]
	} else {
		prefix = strings.TrimSuffix(path, adapterPaths[p.format])
	}
	out := *u
	out.Path = prefix + strings.NewReplacer("{model}", p.model, "{method}", method).Replace(p.cfg.PathOf(apiNames[p.format]))
	out.RawPath = ""
	if p.format == formatMessages {
		// The beta client's ?beta=true isn't the gateway's.
		q := out.Query()
		q.Del("beta")
		out.RawQuery = q.Encode()
	}
	return &out
}

// fetchToken runs the token script; the token is remembered by the
// redactor so it never reaches a log or an error (MDL-108).
func (p *apiProvider) fetchToken(ctx context.Context) (string, error) {
	start := time.Now()
	token, err := runScript(ctx, p.cfg.Language(), p.cfg.TokenScript)
	if err != nil {
		tokenLog().DebugContext(ctx, "gateway token script failed", "language", p.cfg.Language(), "ms", time.Since(start).Milliseconds(), "error", redact.Mask(err.Error()))
		return "", fmt.Errorf("%s %w", p.cfg.Language(), err)
	}
	redact.Remember(token)
	tokenLog().DebugContext(ctx, "gateway token script ran", "language", p.cfg.Language(), "ms", time.Since(start).Milliseconds(), "token_length", len(token))
	return token, nil
}

// gatewayLog is the logger of the gateway's calls and models, and
// tokenLog its tokens': log.levels' "gateway" and "gateway.token" (LOG-01b).
// Neither writes a token: a remembered one is masked, and headers are
// named without their values.
func gatewayLog() *slog.Logger { return observability.Logger("gateway") }
func tokenLog() *slog.Logger   { return observability.Logger("gateway.token") }

// logCall writes one request to the gateway at debug: which API, model
// and URL, why it was sent (a first try, or again with a fresh token or
// without a refused field), how long the reply took to start, and its
// status, with the start of an error's body; or the error that kept it
// from being answered.
func (p *apiProvider) logCall(ctx context.Context, r *http.Request, token, why string, attempt int, took time.Duration, res *http.Response, err error) {
	log := gatewayLog()
	if !log.Enabled(ctx, slog.LevelDebug) {
		return
	}
	u := r.URL.String()
	if token != "" {
		// In the URL it's escaped, which the redactor doesn't know.
		u = strings.NewReplacer(url.QueryEscape(token), config.TokenPlaceholder, token, config.TokenPlaceholder).Replace(u)
	}
	attrs := []any{"api", apiNames[p.format], "model", p.model, "method", r.Method,
		"url", redact.Mask(u), "attempt", attempt, "why", why, "ms", took.Milliseconds()}
	if session := sessionOf(ctx); session != "" {
		attrs = append(attrs, "session", session)
	}
	switch {
	case err != nil:
		attrs = append(attrs, "error", redact.Mask(err.Error()))
	case res.StatusCode >= http.StatusBadRequest:
		attrs = append(attrs, "status", res.StatusCode, "body", redact.Mask(string(peekBody(res))))
	default:
		attrs = append(attrs, "status", res.StatusCode, "content_type", res.Header.Get("Content-Type"))
	}
	log.DebugContext(ctx, "gateway call", attrs...)
}

// middleware signs each request with the session's token, retries once
// with a fresh token when the gateway rejects it, drops the token after a
// connection error or 5xx (MDL-105), sends a request again without a field
// the model refuses (MDL-104b), and fixes up a /responses answer (MDL-106).
func (p *apiProvider) middleware(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
	ctx, session := req.Context(), sessionOf(req.Context())
	body, err := requestBody(req)
	if err != nil {
		return nil, err
	}
	token, err := p.token(ctx, session)
	if err != nil {
		return nil, err
	}
	attempt := 0
	send := func(why string, r *http.Request) (*http.Response, error) {
		attempt++
		start := time.Now()
		res, err := next(r)
		p.logCall(ctx, r, token, why, attempt, time.Since(start), res, err)
		return res, err
	}
	res, err := send("first", p.sign(req, body, token))
	if p.source != nil && err == nil && res.StatusCode == http.StatusUnauthorized {
		// The service no longer takes it (it expired early, or the key
		// that signed it changed): renewed once.
		p.source.Invalidate()
		if fresh, ferr := p.source.AccessToken(ctx); ferr == nil {
			discardResponse(res)
			token = fresh
			res, err = send("the organization's token renewed after 401", p.sign(req, body, fresh))
		}
	}
	if p.tokens != nil {
		switch {
		case err != nil || res.StatusCode >= http.StatusInternalServerError:
			tokenLog().DebugContext(ctx, "gateway token dropped: the call failed, so the next one runs the script")
			p.tokens.Invalidate(session, token)
		case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
			p.tokens.Invalidate(session, token)
			tokenLog().DebugContext(ctx, "gateway token rejected: running the script again", "status", res.StatusCode)
			if fresh, ferr := p.tokens.Token(ctx, session); ferr == nil {
				discardResponse(res)
				token = fresh
				res, err = send(fmt.Sprintf("a fresh token after %d", res.StatusCode), p.sign(req, body, fresh))
			}
		}
	}
	// Each pass drops a new field, so this ends.
	responses := p.format == formatResponses && p.isModelCall(req)
	if err == nil && res.StatusCode == http.StatusBadRequest && responses && p.refusedStream(res) {
		discardResponse(res)
		res, err = send("without stream, which the gateway refused", p.sign(req, body, token))
	}
	for err == nil && res.StatusCode == http.StatusBadRequest && responses && p.refused(res, body) {
		discardResponse(res)
		res, err = send("without "+strings.Join(p.dropped(), ", ")+", which the model refused", p.sign(req, body, token))
	}
	if err == nil && responses && res.StatusCode/100 == 2 {
		switch {
		case streamRequested(body) && isEventStream(res):
			// The adapter asked for a stream (the service's turns do): the
			// events go to it as they come, so the reply shows as it's
			// written rather than when it's whole.
		case streamRequested(body):
			// A gateway that sends replies whole: the adapter reads it as
			// a stream's last event.
			if err = fixResponse(res); err == nil {
				err = asEventStream(res)
			}
		default:
			if err = foldStream(res); err == nil {
				err = fixResponse(res)
			}
		}
		if mp := madePicturesOf(ctx); mp != nil && err == nil {
			// Pictures the model drew, which the adapter refuses (MDL-95a).
			err = mp.rewrite(res)
		}
	}
	keepErrorBody(ctx, res, err)
	return res, err
}

// refused reports whether res refuses a field of body the request can do
// without, which is then left out of the model's requests; res's body is
// left whole for the SDK.
func (p *apiProvider) refused(res *http.Response, body []byte) bool {
	param := unsupportedParam(peekBody(res))
	if !slices.Contains(droppableParams, param) || !hasField(body, param) {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if slices.Contains(p.drop, param) {
		return false
	}
	p.drop = append(p.drop, param)
	return true
}

// refusedStream reports whether res refuses streaming (a gateway that
// doesn't stream the Responses API, as OpenAI refuses a parameter), which
// is then not asked for again (MDL-100c); res's body is left whole.
func (p *apiProvider) refusedStream(res *http.Response) bool {
	if unsupportedParam(peekBody(res)) != "stream" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.unstreamed {
		return false
	}
	p.unstreamed = true
	return true
}

// streams is whether the model's Responses requests ask for a stream.
func (p *apiProvider) streams() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.unstreamed
}

// dropped is the fields the model's requests leave out.
func (p *apiProvider) dropped() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.drop)
}

// peekBody is the start of res's body (maxErrorBody bytes), leaving the
// body whole.
func peekBody(res *http.Response) []byte {
	if res.Body == nil {
		return nil
	}
	head, _ := io.ReadAll(io.LimitReader(res.Body, maxErrorBody))
	res.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(head), res.Body), res.Body}
	return head
}

// maxErrorBody is how much of an error response's body is kept for the
// call's error.
const maxErrorBody = 4096

// errorReply is the last error response of a call: its status and the
// start of its body, which the SDK's error doesn't keep in any shape other
// than {"error": {…}}, nor at all when it can't read the body.
type errorReply struct {
	status int
	body   string
}

// errorBodyKey carries a call's *atomic.Pointer[errorReply].
type errorBodyKey struct{}

// keepErrorBody saves an error response's status and the start of its
// body for the call's error, leaving the body whole for the SDK; any other
// outcome clears it, so an error quotes only the attempt that failed.
func keepErrorBody(ctx context.Context, res *http.Response, err error) {
	kept, _ := ctx.Value(errorBodyKey{}).(*atomic.Pointer[errorReply])
	switch {
	case kept == nil:
	case err == nil && res.StatusCode >= http.StatusBadRequest:
		kept.Store(&errorReply{res.StatusCode, string(peekBody(res))})
	default:
		kept.Store(nil)
	}
}

// token is session's token, or "" when nothing uses one.
func (p *apiProvider) token(ctx context.Context, session string) (string, error) {
	if p.source != nil {
		// The organization's token is remembered by the redactor too, as a
		// script's is (MDL-108): a gateway error echoing it is masked.
		tok, err := p.source.AccessToken(ctx)
		if err == nil {
			redact.Remember(tok)
		}
		return tok, err
	}
	if p.tokens == nil {
		return "", nil
	}
	return p.tokens.Token(ctx, session)
}

// sign is req with body and the configured headers, the placeholder in
// them and in the URL replaced by token (MDL-101, MDL-104).
func (p *apiProvider) sign(req *http.Request, body []byte, token string) *http.Request {
	r := req.Clone(req.Context())
	strip := openAIEnvHeaders
	switch {
	case p.format == formatMessages:
		strip = anthropicAuthHeaders
	case p.format.google():
		strip = googleAuthHeaders
	}
	call := p.isModelCall(r)
	if call {
		r.URL = p.routed(r.URL)
	}
	for _, h := range strip {
		r.Header.Del(h)
	}
	for name, value := range p.cfg.Headers {
		r.Header.Set(name, strings.ReplaceAll(value, config.TokenPlaceholder, token))
	}
	if p.source != nil {
		// Which session and agent the call is for, so the service applies
		// their guardrails and counts their use (EN-22).
		if session := sessionOf(r.Context()); session != "" {
			r.Header.Set("Blitz-Session", session)
		}
		if agent := agentOf(r.Context()); agent != "" {
			r.Header.Set("Blitz-Agent", agent)
		}
	}
	if len(p.query) > 0 || p.tokenInURL {
		r.URL = p.requestURL(r.URL, token)
	}
	if body != nil {
		if p.format == formatResponses && call {
			body = requestBodyFor(body, p.dropped(), p.streams())
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
		r.ContentLength = int64(len(body))
	}
	return r
}

// requestURL is u with base_url's query added and the placeholder (in its
// path or query, escaped either way) replaced by token, escaped.
func (p *apiProvider) requestURL(u *url.URL, token string) *url.URL {
	out := *u
	q := out.Query()
	for k, vs := range p.query {
		q[k] = vs
	}
	out.RawQuery = q.Encode()
	escaped := url.QueryEscape(token)
	s := strings.NewReplacer(escapedPlaceholder, escaped, config.TokenPlaceholder, escaped).Replace(out.String())
	parsed, err := url.Parse(s)
	if err != nil {
		return u
	}
	return parsed
}

// requestBody reads req's body, so it can be sent twice (nil when none).
func requestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	defer req.Body.Close()
	return io.ReadAll(req.Body)
}

// discardResponse closes a response that won't be read.
func discardResponse(res *http.Response) {
	if res != nil && res.Body != nil {
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}
}

// apiModel is the api provider's model: it fetches the session's token
// before any request (a failing script ends the call without one being
// sent, MDL-103) and says what a rejected token means (MDL-105).
type apiModel struct {
	inner model.LLM
	p     *apiProvider
}

// Name is the model's name.
func (m *apiModel) Name() string { return m.inner.Name() }

// GenerateContent runs the call with its session's token.
func (m *apiModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		session := adkSessionID(ctx)
		if _, err := m.p.token(ctx, session); err != nil {
			if m.p.source != nil {
				yield(nil, err)
			} else {
				yield(nil, fmt.Errorf("[llm.api] token script: %w", err))
			}
			return
		}
		var reply atomic.Pointer[errorReply]
		ctx = context.WithValue(withAgent(withSession(ctx, session), adkAgentName(ctx)), errorBodyKey{}, &reply)
		ctx, pictures := withMadePictures(ctx)
		for resp, err := range m.inner.GenerateContent(ctx, req, stream) {
			if err != nil {
				reported := apiError(err, reply.Load(), m.p.source != nil)
				gatewayLog().DebugContext(ctx, "gateway call failed", "model", m.p.model, "error", reported.Error(), "cause", redact.Mask(err.Error()))
				err = reported
			}
			pictures.restore(resp)
			if !yield(resp, err) {
				return
			}
		}
	}
}

// apiError is err as the api provider reports it: a rejected token says
// what to do, another HTTP error quotes the gateway's reason (its
// error.message, else the start of reply's body: the SDK's text is only
// the status, and it has no error at all for a body it can't read), every error is the api provider's ("[llm.api] …", not
// the adapter's "openai: call failed"), and nothing quotes a token (one
// can be in the URL). For an organization's service (org: the enterprise
// provider) the errors are its, and a rejected sign-in says how to sign
// in again: the person has no [llm.api] and no token script.
func apiError(err error, reply *errorReply, org bool) error {
	prefix := "[llm.api] "
	if org {
		prefix = "your organization's service: "
	}
	status, reason, body := 0, "", ""
	if reply != nil {
		status, body = reply.status, reply.body
	}
	var openAIErr *openai.Error
	var anthropicErr *anthropic.Error
	switch {
	case errors.As(err, &openAIErr):
		status, reason = openAIErr.StatusCode, openAIErr.Message
	case errors.As(err, &anthropicErr):
		// Its text quotes the request's URL, which can hold the token.
		status = anthropicErr.StatusCode
	case status == 0:
		return maskedError{prefix + strings.TrimPrefix(err.Error(), adapterPrefix), err}
	}
	if org && (status == http.StatusUnauthorized || status == http.StatusForbidden) {
		return fmt.Errorf("your organization's service refused your sign-in (HTTP %d): sign in again (Settings › Organization, or blitz enterprise login), then retry", status)
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("[llm.api] the API rejected the token (HTTP %d). Check the token script, sign in again if needed, then retry", status)
	}
	// An organization's limit is that, never a model failure (spec_053
	// EN-24): the gateway says whose, what's left and when it resets.
	switch errorCode(body) {
	case "budget_exceeded", "quota_exceeded":
		return maskedError{errorMessage(body) + " Settings › Models (or blitz enterprise usage) shows what's left of each limit; another model may still be within yours.", ErrOrgLimit}
	}
	text := fmt.Sprintf("%sHTTP %d", prefix, status)
	if s := http.StatusText(status); s != "" {
		text += " " + s
	}
	if reason == "" {
		reason = errorMessage(body)
	}
	if reason = scriptStderr(reason); reason != "" {
		text += ": " + reason
	}
	return maskedError{text, err}
}

// errorMessage is an error body's error.message (as OpenAI's, Anthropic's
// and the gateway's own say it), else the body.
func errorMessage(body string) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(body), &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	return body
}

// ErrOrgLimit is a request the organization's gateway refused for a
// budget or quota (spec_053 EN-24).
var ErrOrgLimit = errors.New("your organization's limit is reached")

// errorCode is an error body's error.code (the gateway's), else "".
func errorCode(body string) string {
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal([]byte(body), &e)
	return e.Error.Code
}

// adapterPrefix starts the openai adapter's errors.
const adapterPrefix = "openai: call failed: "

// maskedError is an error with its own text, remembered tokens masked; it
// still unwraps to the original, for errors.Is and errors.As.
type maskedError struct {
	text string
	err  error
}

func (e maskedError) Error() string { return redact.Mask(e.text) }
func (e maskedError) Unwrap() error { return e.err }
