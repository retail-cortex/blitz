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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/redact"
	"google.golang.org/adk/v2/model"
)

// The api provider (spec_015_models §3.4): the adapter for the API the
// model speaks behind the gateway (MDL-100b), with a middleware that signs
// each request with the configured headers and the session's token, and
// fixes up the gateway's answers (apiresponse.go).

// apiFormat is the API a model behind the gateway speaks, which its
// requests are written in (MDL-100b).
type apiFormat int

const (
	// formatResponses is OpenAI's Responses API, through the openai adapter.
	formatResponses apiFormat = iota
	// formatMessages is Anthropic's Messages API (claude-… models), through
	// the anthropic adapter.
	formatMessages
)

// apiFormatOf is the API model speaks: a gateway passes a Claude model's
// request to Anthropic's API as it is.
func apiFormatOf(model string) apiFormat {
	if isClaude(model) {
		return formatMessages
	}
	return formatResponses
}

// apiPlaceholderKey is the SDK's key: never sent (MDL-104), but set so the
// SDK doesn't take OPENAI_API_KEY from the environment.
const apiPlaceholderKey = "unused"

// messagesPath is where the anthropic adapter sends a request: the
// gateway takes it at <base_url>/responses instead.
const messagesPath = "/v1/messages"

// escapedPlaceholder is the placeholder as a URL's string writes it.
var escapedPlaceholder = url.PathEscape(config.TokenPlaceholder)

// openAIEnvHeaders are headers the SDK fills from the environment, removed
// unless configured, so no OpenAI setting reaches the gateway.
var openAIEnvHeaders = []string{"Authorization", "OpenAI-Organization", "OpenAI-Project"}

// anthropicAuthHeaders are the anthropic adapter's credentials (the
// placeholder key), removed unless configured.
var anthropicAuthHeaders = []string{"X-Api-Key", "Authorization"}

// apiProvider signs the api provider's requests.
type apiProvider struct {
	cfg    config.APIConfig
	format apiFormat
	tokens *sessionTokens // nil when nothing uses the placeholder
	// source gives the token in place of a script: the organization's
	// service's sign-in (spec_053_enterprise EN-20), which renews itself.
	source tokenSource
	// messagesAt is where Claude's requests go, under the base URL:
	// /responses on a gateway that takes every model's there, /messages on
	// the organization's service.
	messagesAt string
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
	if err := c.Check(); err != nil {
		return nil, err
	}
	p := &apiProvider{cfg: c, format: apiFormatOf(name)}
	if c.UsesToken() {
		p.tokens = newSessionTokens(p.fetchToken)
	}
	return buildAPIModel(ctx, p, name, pol)
}

// buildAPIModel is the model p signs the requests of.
func buildAPIModel(ctx context.Context, p *apiProvider, name string, pol retryPolicy) (model.LLM, error) {
	c := p.cfg
	p.tokenInURL = strings.Contains(c.BaseURL, config.TokenPlaceholder)
	if p.messagesAt == "" {
		p.messagesAt = "/responses"
	}
	if p.format == formatResponses && rejectsSampling(name) {
		p.drop = slices.Clone(samplingParams)
	}
	// The SDK resolves "responses" against the base URL, which drops its
	// query: that goes on each request instead (sign).
	base, rawQuery, _ := strings.Cut(c.BaseURL, "?")
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, fmt.Errorf("[llm.api] base_url's query: %w", err)
	}
	p.query = query
	baseURL := strings.ReplaceAll(base, config.TokenPlaceholder, escapedPlaceholder)
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
	opts := append(pol.openAIOptions(), option.WithMiddleware(p.middleware))
	inner, err := newOpenAIModel(ctx, name, apiPlaceholderKey, baseURL, opts...)
	if err != nil {
		return nil, err
	}
	if t, ok := inner.(*toolCallParsingModel); ok {
		t.streams = true // a gateway's models make real function calls
	}
	return &apiModel{inner: inner, p: p}, nil
}

// isModelCall reports whether req asks for a model response, in the
// model's API.
func (p *apiProvider) isModelCall(req *http.Request) bool {
	path := strings.TrimSuffix(req.URL.Path, "/")
	if p.format == formatMessages {
		return req.Method == http.MethodPost && strings.HasSuffix(path, messagesPath)
	}
	return req.Method == http.MethodPost && strings.HasSuffix(path, "/responses")
}

// fetchToken runs the token script; the token is remembered by the
// redactor so it never reaches a log or an error (MDL-108).
func (p *apiProvider) fetchToken(ctx context.Context) (string, error) {
	token, err := runScript(ctx, p.cfg.Language(), p.cfg.TokenScript)
	if err != nil {
		return "", fmt.Errorf("%s %w", p.cfg.Language(), err)
	}
	redact.Remember(token)
	return token, nil
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
	res, err := next(p.sign(req, body, token))
	if p.source != nil && err == nil && res.StatusCode == http.StatusUnauthorized {
		// The service no longer takes it (it expired early, or the key
		// that signed it changed): renewed once.
		p.source.Invalidate()
		if fresh, ferr := p.source.AccessToken(ctx); ferr == nil {
			discardResponse(res)
			token = fresh
			res, err = next(p.sign(req, body, fresh))
		}
	}
	if p.tokens != nil {
		switch {
		case err != nil || res.StatusCode >= http.StatusInternalServerError:
			p.tokens.Invalidate(session, token)
		case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
			p.tokens.Invalidate(session, token)
			if fresh, ferr := p.tokens.Token(ctx, session); ferr == nil {
				discardResponse(res)
				token = fresh
				res, err = next(p.sign(req, body, fresh))
			}
		}
	}
	// Each pass drops a new field, so this ends.
	responses := p.format == formatResponses && p.isModelCall(req)
	if err == nil && res.StatusCode == http.StatusBadRequest && responses && p.refusedStream(res) {
		discardResponse(res)
		res, err = next(p.sign(req, body, token))
	}
	for err == nil && res.StatusCode == http.StatusBadRequest && responses && p.refused(res, body) {
		discardResponse(res)
		res, err = next(p.sign(req, body, token))
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
	if p.format == formatMessages {
		strip = anthropicAuthHeaders
		if p.isModelCall(r) {
			// The gateway takes Claude's requests where it takes the others;
			// the beta client's ?beta=true isn't the gateway's.
			u := *r.URL
			u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), messagesPath) + p.messagesAt
			u.RawPath = ""
			q := u.Query()
			q.Del("beta")
			u.RawQuery = q.Encode()
			r.URL = &u
		}
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
		if p.format == formatResponses && p.isModelCall(r) {
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
		for resp, err := range m.inner.GenerateContent(ctx, req, stream) {
			if err != nil {
				err = apiError(err, reply.Load(), m.p.source != nil)
			}
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
