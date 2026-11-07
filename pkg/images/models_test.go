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

package images

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The catalog, and a model it doesn't know (spec_058_pictures PIC-01..03).
func TestLookup(t *testing.T) {
	m, ok := Lookup("gemini/gemini-3.1-flash-image")
	require.True(t, ok)
	assert.Equal(t, KindGemini, m.Kind)
	assert.Contains(t, m.Sizes, "0.5K")
	assert.Contains(t, m.Ratios, "8:1")
	assert.False(t, m.Assumed)
	assert.Equal(t, "gemini-nano-banana-2.1", Models()[0].ID, "the default first")

	im, ok := Lookup("imagen-5.0-generate")
	require.True(t, ok)
	assert.True(t, im.Assumed)
	assert.Equal(t, KindImagen, im.Kind)
	g, ok := Lookup("gemini-9-flash-image")
	require.True(t, ok)
	assert.True(t, g.Assumed)
	assert.Equal(t, 3, g.MaxReferences, "its family's most careful")
	_, ok = Lookup("gemini-4-argon")
	assert.False(t, ok)

	assert.Equal(t, 2520, m.OutputTokens("4K"))
	assert.Equal(t, 1120, m.OutputTokens("9K"), "1K's when the size isn't known")
	lite, _ := Lookup("gemini-3.1-flash-lite-image")
	assert.Equal(t, 1120, lite.OutputTokens(""))
}

// What a model doesn't take, said with what it does (PIC-62).
func TestCheck(t *testing.T) {
	nano, _ := Lookup("gemini-nano-banana-2.1")
	imagen, _ := Lookup("imagen-3.0-generate-002")
	lite, _ := Lookup("gemini-3.1-flash-lite-image")
	assert.Empty(t, nano.Check(Request{Prompt: "a city", AspectRatio: "21:9", Size: "4K", Thinking: "HIGH", Search: true, SearchImages: true, Format: "image/webp", Quality: 90,
		References: []Reference{{Role: RoleCharacter, MIME: "image/png"}, {Role: RoleStyle, MIME: "image/webp"}}}))
	assert.Empty(t, imagen.Check(Request{Prompt: "a fox", Count: 4, AspectRatio: "16:9", Avoid: "blur", People: "ALLOW_ADULT", Seed: true, Enhance: true}))

	chars := make([]Reference, 5)
	for i := range chars {
		chars[i] = Reference{Role: RoleCharacter, MIME: "image/png"}
	}
	objects := make([]Reference, 11)
	for i := range objects {
		objects[i] = Reference{Role: RoleObject, MIME: "image/png"}
	}
	for _, tc := range []struct {
		name string
		m    Model
		r    Request
		want string
	}{
		{"no prompt", nano, Request{Prompt: " "}, "say what the picture shows"},
		{"imagen prompt too long", imagen, Request{Prompt: strings.Repeat("word ", 500)}, "longer than Imagen 3 takes"},
		{"imagen has no references", imagen, Request{Prompt: "x", References: []Reference{{MIME: "image/png"}}}, "doesn't start from pictures"},
		{"too many references", lite, Request{Prompt: "x", References: []Reference{{MIME: "image/png"}, {MIME: "image/png"}}}, "at most 1 reference"},
		{"a fifth character", nano, Request{Prompt: "x", References: chars}, "at most 4 character"},
		{"an eleventh object", nano, Request{Prompt: "x", References: objects[:11]}, "at most 10 object"},
		{"a role", nano, Request{Prompt: "x", References: []Reference{{Role: "hero", MIME: "image/png"}}}, "isn't a role"},
		{"a type", nano, Request{Prompt: "x", References: []Reference{{MIME: "image/gif"}}}, "PNG, JPEG or WebP"},
		{"too big", nano, Request{Prompt: "x", References: []Reference{{MIME: "image/png", Size: MaxReferenceBytes + 1}}}, "larger than 7 MB"},
		{"gemini makes one", nano, Request{Prompt: "x", Count: 2}, "at most 1 picture"},
		{"a ratio", imagen, Request{Prompt: "x", AspectRatio: "21:9"}, "doesn't make 21:9"},
		{"a size", nano, Request{Prompt: "x", Size: "0.5K"}, "doesn't make 0.5K"},
		{"one size", imagen, Request{Prompt: "x", Size: "2K"}, "has one size"},
		{"thinking", lite, Request{Prompt: "x", Thinking: "HIGH"}, "thinking level"},
		{"search", lite, Request{Prompt: "x", Search: true}, "doesn't search the web"},
		{"image search", imagen, Request{Prompt: "x", SearchImages: true}, "doesn't search images"},
		{"avoid", nano, Request{Prompt: "x", Avoid: "blur"}, "in the prompt itself"},
		{"people", nano, Request{Prompt: "x", People: "ALLOW_ALL"}, "people setting"},
		{"seed", nano, Request{Prompt: "x", Seed: true}, "doesn't take a seed"},
		{"enhance", nano, Request{Prompt: "x", Enhance: true}, "doesn't improve prompts"},
		{"format", lite, Request{Prompt: "x", Format: "image/webp"}, "doesn't make image/webp"},
		{"quality", nano, Request{Prompt: "x", Quality: 101}, "quality is 0 to 100"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Contains(t, strings.Join(tc.m.Check(tc.r), "; "), tc.want)
		})
	}
}
