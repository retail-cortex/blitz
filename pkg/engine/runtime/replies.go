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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/openai/openai-go/v3/option"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// Replies as Blitz keeps them (spec_056_conversation_adapters §6, §7):
// every part a model returns is kept or noted, never an error; why a reply
// ended is normalised; a context too long is summarised away; and each
// call streams when the model can.

// UnshownKey is the CustomMetadata key of the parts of an answer Blitz
// doesn't show yet (a provider's server tool blocks, output it has no
// form for): each {"type", "raw"}, kept with the answer, noted to the
// person (CA-30).
const UnshownKey = "blitz_unshown"

// unshownParts collects what an adapter's transport took out of a reply
// before the SDK could refuse it.
type unshownParts struct {
	mu    sync.Mutex
	parts []map[string]any
}

func (u *unshownParts) add(item map[string]any) {
	if u == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	typ, _ := item["type"].(string)
	u.parts = append(u.parts, map[string]any{"type": typ, "raw": item})
}

func (u *unshownParts) take() []map[string]any {
	if u == nil {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	out := u.parts
	u.parts = nil
	return out
}

type unshownKey struct{}

func withUnshown(ctx context.Context) (context.Context, *unshownParts) {
	u := &unshownParts{}
	return context.WithValue(ctx, unshownKey{}, u), u
}

// attachUnshown records parts on resp's metadata.
func attachUnshown(resp *model.LLMResponse, parts []map[string]any) {
	if resp == nil || len(parts) == 0 {
		return
	}
	if resp.CustomMetadata == nil {
		resp.CustomMetadata = map[string]any{}
	}
	prev, _ := resp.CustomMetadata[UnshownKey].([]map[string]any)
	resp.CustomMetadata[UnshownKey] = append(prev, parts...)
}

// unshownTypes are the types of the parts resp couldn't show.
func unshownTypes(resp *model.LLMResponse) []string {
	if resp == nil {
		return nil
	}
	var out []string
	switch v := resp.CustomMetadata[UnshownKey].(type) {
	case []map[string]any:
		for _, p := range v {
			if t, _ := p["type"].(string); t != "" && !slices.Contains(out, t) {
				out = append(out, t)
			}
		}
	case []any: // read back from a session's JSON
		for _, p := range v {
			m, _ := p.(map[string]any)
			if t, _ := m["type"].(string); t != "" && !slices.Contains(out, t) {
				out = append(out, t)
			}
		}
	}
	return out
}

// The Responses API's output items and message contents the OpenAI
// adapter (ADK's) reads; it refuses a reply holding any other.
var (
	responsesItems    = map[string]bool{"message": true, "function_call": true, "reasoning": true}
	responsesContents = map[string]bool{"output_text": true, "refusal": true}
)

// sanitizeResponses takes the parts the OpenAI adapter can't read out of a
// Responses API document (a reply, or one of its stream's events), handing
// each to add. drop is true for a stream event about such a part, which
// goes as a whole.
func sanitizeResponses(doc map[string]any, add func(map[string]any)) (drop bool) {
	if item, ok := doc["item"].(map[string]any); ok {
		if t, _ := item["type"].(string); !responsesItems[t] {
			if strings.HasSuffix(fmt.Sprint(doc["type"]), ".done") {
				add(item) // once, when it's whole
			}
			return true
		}
		sanitizeMessage(item, add)
	}
	if part, ok := doc["part"].(map[string]any); ok {
		if t, _ := part["type"].(string); !responsesContents[t] {
			return true
		}
	}
	if resp, ok := doc["response"].(map[string]any); ok {
		sanitizeResponses(resp, func(map[string]any) {}) // its parts were noted as their items finished
	}
	if out, ok := doc["output"].([]any); ok {
		kept := out[:0:0]
		for _, o := range out {
			item, _ := o.(map[string]any)
			if t, _ := item["type"].(string); !responsesItems[t] {
				add(item)
				continue
			}
			sanitizeMessage(item, add)
			kept = append(kept, item)
		}
		doc["output"] = kept
	}
	return false
}

func sanitizeMessage(item map[string]any, add func(map[string]any)) {
	if t, _ := item["type"].(string); t != "message" {
		return
	}
	content, ok := item["content"].([]any)
	if !ok {
		return
	}
	kept := content[:0:0]
	for _, c := range content {
		part, _ := c.(map[string]any)
		if t, _ := part["type"].(string); !responsesContents[t] {
			add(part)
			continue
		}
		kept = append(kept, part)
	}
	item["content"] = kept
}

// openAIOutputMiddleware takes what the OpenAI adapter can't read out of a
// Responses API reply, whole or streamed, and keeps it for the answer
// (CA-30): the adapter would fail the call on it. A server that answers a
// streamed request whole (some OpenAI-compatible ones ignore "stream") has
// its reply passed on as the stream's last event, which the adapter
// rebuilds the answer from; read as a stream, it would be no answer at all.
func openAIOutputMiddleware(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
	if !strings.HasSuffix(req.URL.Path, "/responses") {
		return next(req)
	}
	streamed := asksForStream(req)
	resp, err := next(req)
	if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
		return resp, err
	}
	u, _ := req.Context().Value(unshownKey{}).(*unshownParts)
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		resp.Body = sanitizeStream(resp.Body, u.add)
		return resp, nil
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if json.Unmarshal(body, &doc) == nil {
		removed := false
		sanitizeResponses(doc, func(item map[string]any) { removed = true; u.add(item) })
		if removed || streamed {
			body, _ = json.Marshal(doc)
		}
		if streamed {
			event, _ := json.Marshal(map[string]any{"type": "response.completed", "sequence_number": 0, "response": doc})
			body = []byte("event: response.completed\ndata: " + string(event) + "\n\n")
			resp.Header.Set("Content-Type", "text/event-stream")
		}
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	return resp, nil
}

// asksForStream reports whether req's body asks for a stream, leaving the
// body to be sent as it was.
func asksForStream(req *http.Request) bool {
	if req.Body == nil {
		return false
	}
	body, err := io.ReadAll(req.Body)
	req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return false
	}
	var doc struct {
		Stream bool `json:"stream"`
	}
	return json.Unmarshal(body, &doc) == nil && doc.Stream
}

// sanitizeStream is body, a stream of server-sent events, with each
// event's unreadable parts taken out, and events about them dropped.
func sanitizeStream(body io.ReadCloser, add func(map[string]any)) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		defer body.Close()
		sc := bufio.NewScanner(body)
		sc.Buffer(make([]byte, 64<<10), 64<<20)
		var event []string
		flush := func() error {
			defer func() { event = event[:0] }()
			if len(event) == 0 {
				return nil
			}
			for i, line := range event {
				data, ok := strings.CutPrefix(line, "data: ")
				if !ok {
					continue
				}
				var doc map[string]any
				if json.Unmarshal([]byte(data), &doc) != nil {
					continue
				}
				if sanitizeResponses(doc, add) {
					return nil
				}
				b, _ := json.Marshal(doc)
				event[i] = "data: " + string(b)
			}
			_, err := io.WriteString(pw, strings.Join(event, "\n")+"\n\n")
			return err
		}
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				if err := flush(); err != nil {
					return
				}
				continue
			}
			event = append(event, line)
		}
		if err := flush(); err != nil {
			return
		}
		pw.CloseWithError(sc.Err())
	}()
	return pr
}

// Errors, as each wire API words them (CA-32).

// contextTooLong matches each API's words for a request longer than the
// model's context window: Anthropic's, OpenAI's, Gemini's and the
// OpenAI-compatible servers'.
var contextTooLong = regexp.MustCompile(`(?i)prompt is too long|input length and .max_tokens. exceed context limit|context_length_exceeded|maximum context length|exceeds the context window|input token count.*exceeds|exceeds the maximum number of tokens|context length exceeded|too many tokens`)

// asContextLength is err as api.ErrContextLength when it says the request
// was longer than the model reads, keeping the provider's words.
func asContextLength(err error) error {
	if err == nil || errors.Is(err, api.ErrContextLength) || !contextTooLong.MatchString(err.Error()) {
		return err
	}
	return &contextLengthError{err: err}
}

// contextLengthError is a request too long for the model: errors.Is it
// api.ErrContextLength, with the provider's own error kept.
type contextLengthError struct{ err error }

func (e *contextLengthError) Error() string { return e.err.Error() }

func (e *contextLengthError) Is(target error) bool { return target == api.ErrContextLength }

func (e *contextLengthError) Unwrap() error { return e.err }

// compactedRetryKey marks a call made again after the conversation was
// summarised to fit (CA-111): a fallback chain moves on from a context too
// long only then.
type compactedRetryKey struct{}

func withCompactedRetry(ctx context.Context) context.Context {
	return context.WithValue(ctx, compactedRetryKey{}, true)
}

func compactedRetry(ctx context.Context) bool {
	v, _ := ctx.Value(compactedRetryKey{}).(bool)
	return v
}

// Why a reply ended (CA-31).

// finishError is the error a turn ends with when its last model reply
// ended as resp did (nil when it ended well): cut off, refused, blocked,
// stopped for recitation, or a tool call that couldn't be read.
func finishError(resp *model.LLMResponse) error {
	switch resp.FinishReason {
	case genai.FinishReasonMaxTokens:
		return api.ErrOutputLimit
	case genai.FinishReasonSafety:
		if resp.ErrorCode == "refusal" {
			return api.ErrRefused
		}
		return api.ErrBlocked
	case genai.FinishReasonProhibitedContent, genai.FinishReasonBlocklist, genai.FinishReasonSPII,
		genai.FinishReasonImageSafety, genai.FinishReasonImageProhibitedContent:
		return api.ErrBlocked
	case genai.FinishReasonRecitation, genai.FinishReasonImageRecitation:
		return api.ErrRecitation
	case genai.FinishReasonMalformedFunctionCall, genai.FinishReasonUnexpectedToolCall:
		return api.ErrMalformedToolCall
	}
	return nil
}

// malformedCall reports whether resp is a whole reply whose tool call the
// provider couldn't read (Gemini's MALFORMED_FUNCTION_CALL).
func malformedCall(resp *model.LLMResponse) bool {
	return resp != nil && !resp.Partial &&
		(resp.FinishReason == genai.FinishReasonMalformedFunctionCall || resp.FinishReason == genai.FinishReasonUnexpectedToolCall)
}

// retryMalformed calls gen, and once more with a note to the model when
// its reply is a tool call that couldn't be read (CA-31); the second such
// reply goes on, and ends the turn with MALFORMED_TOOL_CALL.
func retryMalformed(req *model.LLMRequest, gen func(*model.LLMRequest) iter.Seq2[*model.LLMResponse, error]) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		for resp, err := range gen(req) {
			if err == nil && malformedCall(resp) && req != nil {
				cp := *req
				cp.Contents = append(slices.Clone(req.Contents), genai.NewContentFromText(i18n.T("model.malformed_retry"), genai.RoleUser))
				for resp, err := range gen(&cp) {
					if !yield(resp, err) {
						return
					}
				}
				return
			}
			if !yield(resp, err) {
				return
			}
		}
	}
}

// Streaming, decided per call (CA-50–CA-53).

// unstreamed are the models that refused to stream (a 400 on "stream"),
// called whole from then on (CA-53).
var unstreamed sync.Map

// streamsFor reports whether a call to model may stream. Each model
// streams unless it refused to; the model record (CA-10) will say so for
// models known not to.
func streamsFor(model string) bool {
	_, refused := unstreamed.Load(model)
	return !refused
}

// refusesStreaming matches a provider's words for a request whose "stream"
// it doesn't take.
var refusesStreaming = regexp.MustCompile(`(?i)stream(ing)?[^.]*(not supported|unsupported|isn't supported|is not available|not allowed)|unsupported (parameter|value)[^.]*stream|unknown (field|parameter)[^.]*stream`)

// streamingModel streams each call asked to stream that the model can
// stream (CA-50); a model that refuses to stream is called whole from
// then on (CA-53).
type streamingModel struct {
	inner model.LLM
}

func (m *streamingModel) Name() string { return m.inner.Name() }

func (m *streamingModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		name := m.inner.Name()
		if !streamsFor(name) {
			for resp, err := range m.inner.GenerateContent(ctx, req, false) {
				if !yield(resp, err) {
					return
				}
			}
			return
		}
		sent := false
		for resp, err := range m.inner.GenerateContent(ctx, req, stream) {
			if err != nil && stream && !sent && refusesStreaming.MatchString(err.Error()) {
				unstreamed.Store(name, true)
				for resp, err := range m.inner.GenerateContent(ctx, req, false) {
					if !yield(resp, err) {
						return
					}
				}
				return
			}
			sent = sent || resp != nil
			if !yield(resp, err) {
				return
			}
		}
	}
}

// noteUnshown tells the person once per answer what a model returned that
// Blitz doesn't show yet (CA-30).
func (e *Engine) noteUnshown(ctx context.Context, resp *model.LLMResponse) {
	types := unshownTypes(resp)
	if len(types) == 0 {
		return
	}
	if notice := e.noticeTo(ctx); notice != nil {
		name := resp.ModelVersion
		if o, ok := originOf(resp); ok {
			name = o.Model
		}
		notice(i18n.T("model.unshown", "model", name, "types", strings.Join(types, ", ")))
	}
}
