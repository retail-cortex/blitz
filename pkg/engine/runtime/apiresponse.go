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
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The api provider's bodies (spec_models_015 MDL-104, MDL-106): requests
// never stored or streamed and without what the model refuses, and the
// gateway's answers made into what the openai adapter reads. Only the
// fields named change; the rest is kept as the bytes it was.

// samplingParams are the request fields a model that rejects sampling
// refuses.
var samplingParams = []string{"temperature", "top_p"}

// droppableParams are the request fields the api provider leaves out once
// the gateway refuses them for the model (MDL-104b): tuning the request
// can do without, never what it says.
var droppableParams = []string{"temperature", "top_p", "reasoning", "max_output_tokens", "parallel_tool_calls"}

// rejectsSampling reports whether a model behind a gateway refuses
// temperature and top_p: current Claude models (as for the anthropic
// provider) and OpenAI's reasoning models.
func rejectsSampling(model string) bool {
	m := strings.ToLower(model)
	if strings.HasPrefix(m, "claude-") {
		return !supportsSampling(m)
	}
	for _, p := range []string{"gpt-5", "o1", "o3", "o4"} {
		if strings.HasPrefix(m, p) {
			return true
		}
	}
	return false
}

// unsupportedParamText finds the field in OpenAI's refusal message.
var unsupportedParamText = regexp.MustCompile(`Unsupported (?:parameter|value): '([^']+)'`)

// unsupportedParam is the request field a 400's body says the model
// doesn't take: OpenAI's code unsupported_parameter or unsupported_value
// with its param, else the field its message names; "" when it says
// neither.
func unsupportedParam(errBody []byte) string {
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Param   string `json:"param"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(errBody, &e) != nil {
		return ""
	}
	if (e.Error.Code == "unsupported_parameter" || e.Error.Code == "unsupported_value") && e.Error.Param != "" {
		return e.Error.Param
	}
	if m := unsupportedParamText.FindStringSubmatch(e.Error.Message); m != nil {
		return m[1]
	}
	return ""
}

// hasField reports whether body is a JSON object with the field.
func hasField(body []byte, field string) bool {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(body, &m)
	_, ok := m[field]
	return ok
}

// requestBodyFor is a /responses request body with "store" and "stream"
// false and the fields in drop left out; a body that isn't a JSON object
// is left as it is.
func requestBodyFor(body []byte, drop []string) []byte {
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil || m == nil {
		return body
	}
	m["store"], m["stream"] = json.RawMessage("false"), json.RawMessage("false")
	for k := range m {
		if slices.Contains(drop, k) {
			delete(m, k)
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

// fixResponse makes a successful /responses answer what the adapter
// reads: an error message under any status is a failure, and a message's
// "text" content is "output_text". A body that isn't JSON is left alone.
func fixResponse(res *http.Response) error {
	if res.Body == nil {
		return nil
	}
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		return err
	}
	if fixed, changed := fixResponseBody(body); changed {
		body = fixed
		res.ContentLength = int64(len(body))
		res.Header.Set("Content-Length", strconv.Itoa(len(body)))
	}
	res.Body = io.NopCloser(bytes.NewReader(body))
	return nil
}

// fixResponseBody is body fixed as fixResponse says, and whether anything
// changed.
func fixResponseBody(body []byte) ([]byte, bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil || m == nil {
		return body, false
	}
	changed := false
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(m["error"], &e) == nil && e.Message != "" && string(m["status"]) != `"failed"` {
		m["status"], changed = json.RawMessage(`"failed"`), true
	}
	var output []map[string]json.RawMessage
	if json.Unmarshal(m["output"], &output) == nil {
		outputChanged := false
		for _, item := range output {
			if string(item["type"]) == `"message"` && fixTextParts(item) {
				outputChanged = true
			}
		}
		if outputChanged {
			if b, err := json.Marshal(output); err == nil {
				m["output"], changed = b, true
			}
		}
	}
	if !changed {
		return body, false
	}
	out, err := json.Marshal(m)
	if err != nil {
		return body, false
	}
	return out, true
}

// fixTextParts renames a message item's "text" content parts to
// "output_text", reporting whether it changed any.
func fixTextParts(item map[string]json.RawMessage) bool {
	var content []map[string]json.RawMessage
	if json.Unmarshal(item["content"], &content) != nil {
		return false
	}
	changed := false
	for _, part := range content {
		if string(part["type"]) == `"text"` {
			part["type"], changed = json.RawMessage(`"output_text"`), true
		}
	}
	if !changed {
		return false
	}
	b, err := json.Marshal(content)
	if err != nil {
		return false
	}
	item["content"] = b
	return true
}
