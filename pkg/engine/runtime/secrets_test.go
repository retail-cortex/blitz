// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func secretEngine() *Engine {
	return &Engine{secrets: func(text string) (string, []string) {
		if text == "use {secrets:token}" {
			return "use engine-secret-42", []string{"engine-secret-42"}
		}
		return text, nil
	}}
}

// Messages naming a secret are copied with the value in; the session's
// own stay as they were; others are passed as they are.
func TestExpandContents(t *testing.T) {
	e := secretEngine()
	plain := genai.NewContentFromText("hello", genai.RoleUser)
	named := genai.NewContentFromText("use {secrets:token}", genai.RoleUser)
	contents := []*genai.Content{plain, named, nil}
	out := e.expandContents(contents)
	assert.Same(t, plain, out[0], "untouched")
	assert.Equal(t, "use engine-secret-42", out[1].Parts[0].Text)
	assert.Equal(t, "use {secrets:token}", named.Parts[0].Text, "the session's copy keeps the name")
	assert.Nil(t, out[2])

	same := []*genai.Content{plain}
	assert.Equal(t, same, e.expandContents(same), "nothing to expand: the same slice")
	assert.Equal(t, contents, (&Engine{}).expandContents(contents), "no secrets: as they are")
	assert.Equal(t, "use engine-secret-42", e.expandSecrets("use {secrets:token}"))
	assert.Equal(t, "x", (&Engine{}).expandSecrets("x"))
}

// A value the model echoes is masked in its answer, partial or not.
func TestMaskSecrets(t *testing.T) {
	e := secretEngine()
	e.expandSecrets("use {secrets:token}") // remembered as it's used
	resp := &model.LLMResponse{Partial: true, Content: genai.NewContentFromText("it was engine-secret-42", genai.RoleModel)}
	_, err := e.afterModel(nil, resp, nil)
	assert.NoError(t, err)
	assert.Equal(t, "it was [REDACTED]", resp.Content.Parts[0].Text)
	maskSecrets(nil)
	maskSecrets(&model.LLMResponse{})
}
