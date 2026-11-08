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
	"fmt"
	"slices"
	"strings"
)

// The kinds of image model (spec_058_pictures §2).
const (
	// KindGemini answers with text and one picture a turn, takes
	// references and keeps a conversation (generateContent).
	KindGemini = "gemini"
	// KindImagen makes 1 to 4 pictures from text alone (generateImages).
	KindImagen = "imagen"
	// KindOpenAI is an OpenAI image model: 1 to 4 pictures a request,
	// from text (images/generations) or starting from references
	// (images/edits), priced per picture (spec_058_pictures PIC-04).
	KindOpenAI = "openai"
)

// OpenAIRatios are the shapes an OpenAI image model makes, and their sizes.
var OpenAIRatios = []string{"1:1", "3:2", "2:3"}

// Reference roles (PIC-23).
const (
	RoleCharacter = "character"
	RoleObject    = "object"
	RoleStyle     = "style"
)

// Ratio sets (PIC-02).
var (
	StandardRatios  = []string{"1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9"}
	PanoramicRatios = []string{"1:4", "4:1", "1:8", "8:1"}
	ImagenRatios    = []string{"1:1", "3:4", "4:3", "9:16", "16:9"}
)

// ReferenceTypes are the pictures a model starts from, and MaxReferenceBytes
// the most each may be (PIC-23).
var ReferenceTypes = []string{"image/png", "image/jpeg", "image/webp"}

// MaxReferenceBytes is the most a reference picture may be.
const MaxReferenceBytes = 7 << 20

// ImagenPromptTokens is the most an Imagen prompt may be, in tokens.
const ImagenPromptTokens = 480

// Model is what an image model can do: what Pictures offers for it
// (spec_058_pictures PIC-01). It's data: the panel shows, locks and hides
// its controls from it, and the service checks requests against it.
type Model struct {
	ID   string
	Name string
	Kind string
	// Provider is whose credentials make it ("": gemini).
	Provider string
	// MaxReferences is the most references in all; MaxCharacters and
	// MaxObjects the most of each role (0: no cap of their own).
	MaxReferences int
	MaxCharacters int
	MaxObjects    int
	// MaxPictures is the most pictures a request makes.
	MaxPictures int
	// Sizes are the output sizes ("1K"…; none: the model's own, fixed).
	Sizes  []string
	Ratios []string
	// Thinking are its levels (MINIMAL, MEDIUM, HIGH; none: it doesn't
	// think, or not as a setting); ThinkingAlways: it always thinks.
	Thinking       []string
	ThinkingAlways bool
	// Search is its grounding: "" none, "web", or "image" (web and image).
	Search         string
	NegativePrompt bool
	People         bool
	Seed           bool
	Enhance        bool
	Formats        []string
	// SizeTokens are a picture's output tokens by size (Gemini), for
	// estimates; PerPicture is a picture's price in USD (Imagen).
	SizeTokens map[string]int
	PerPicture float64
	// SearchPerCall is a grounding search's price in USD (0: not known).
	SearchPerCall float64
	// Assumed marks a model the catalog doesn't know: its family's values.
	Assumed bool
}

var gemini3Ratios = append(slices.Clone(StandardRatios), PanoramicRatios...)

// catalog is what each known model can do, as the owner gave it on
// 2026-11-07 (spec_058 PIC-02).
var catalog = []Model{
	{ID: "gemini-nano-banana-2.1", Name: "Gemini Nano Banana 2.1", Kind: KindGemini, MaxReferences: 14, MaxCharacters: 4, MaxObjects: 10, MaxPictures: 1,
		Sizes: []string{"1K", "2K", "4K"}, Ratios: gemini3Ratios, Thinking: []string{"MINIMAL", "MEDIUM", "HIGH"}, Search: "image",
		Formats: []string{"image/png", "image/jpeg", "image/webp"}, SizeTokens: map[string]int{"1K": 1120, "2K": 1680, "4K": 3767}, SearchPerCall: 0.014},
	{ID: "gemini-3-pro-image", Name: "Gemini 3 Pro Image", Kind: KindGemini, MaxReferences: 14, MaxCharacters: 4, MaxObjects: 6, MaxPictures: 1,
		Sizes: []string{"1K", "2K", "4K"}, Ratios: gemini3Ratios, Thinking: []string{"MEDIUM", "HIGH"}, ThinkingAlways: true, Search: "web",
		Formats: []string{"image/png", "image/jpeg", "image/webp"}, SizeTokens: map[string]int{"1K": 1120, "2K": 1120, "4K": 2000}},
	{ID: "gemini-3.1-flash-image", Name: "Gemini 3.1 Flash Image", Kind: KindGemini, MaxReferences: 14, MaxCharacters: 4, MaxObjects: 10, MaxPictures: 1,
		Sizes: []string{"0.5K", "1K", "2K", "4K"}, Ratios: gemini3Ratios, Thinking: []string{"MINIMAL", "MEDIUM", "HIGH"}, Search: "image",
		Formats: []string{"image/png", "image/jpeg", "image/webp"}, SizeTokens: map[string]int{"0.5K": 560, "1K": 1120, "2K": 1680, "4K": 2520}},
	{ID: "gemini-3.1-flash-lite-image", Name: "Gemini 3.1 Flash Lite Image", Kind: KindGemini, MaxReferences: 1, MaxPictures: 1,
		Ratios: StandardRatios, Formats: []string{"image/png", "image/jpeg"}, SizeTokens: map[string]int{"": 1120}},
	{ID: "gemini-2.5-flash-image", Name: "Gemini 2.5 Flash Image", Kind: KindGemini, MaxReferences: 14, MaxPictures: 1,
		Sizes: []string{"1K", "2K"}, Ratios: StandardRatios, Formats: []string{"image/png", "image/jpeg"}, SizeTokens: map[string]int{"1K": 1290, "2K": 1290}},
	{ID: "imagen-4.0-generate-preview", Name: "Imagen 4 Preview", Kind: KindImagen, MaxPictures: 4, Sizes: []string{"1K", "2K"}, Ratios: ImagenRatios,
		NegativePrompt: true, People: true, Seed: true, Enhance: true, Formats: []string{"image/png", "image/jpeg"}, PerPicture: 0.04},
	{ID: "imagen-3.0-generate-002", Name: "Imagen 3", Kind: KindImagen, MaxPictures: 4, Ratios: ImagenRatios,
		NegativePrompt: true, People: true, Seed: true, Enhance: true, Formats: []string{"image/png", "image/jpeg"}, PerPicture: 0.03},
	{ID: "imagen-3.0-fast-generate-001", Name: "Imagen 3 Fast", Kind: KindImagen, MaxPictures: 4, Ratios: ImagenRatios,
		NegativePrompt: true, People: true, Seed: true, Formats: []string{"image/png", "image/jpeg"}, PerPicture: 0.02},
	// OpenAI's (2026-11-08, the owner: "image generation should be supported
	// from Open AI"): their names from the SDK; their prices aren't in its
	// catalog yet, so each is the medium-quality square's, marked Assumed.
	openAIModel("gpt-image-2.5-sunburst", "GPT Image 2.5 Sunburst"),
	openAIModel("gpt-image-2.5-flare", "GPT Image 2.5 Flare"),
	openAIModel("gpt-image-2", "GPT Image 2"),
	openAIModel("gpt-image-1.5", "GPT Image 1.5"),
	openAIModel("gpt-image-1", "GPT Image 1"),
}

// openAIModel is an OpenAI image model's entry: its own references (up to
// 16, for an edit), up to 4 pictures, three shapes, priced per picture.
func openAIModel(id, name string) Model {
	return Model{ID: id, Name: name, Kind: KindOpenAI, Provider: "openai", MaxReferences: 16, MaxPictures: 4, Ratios: OpenAIRatios,
		Formats: []string{"image/png", "image/jpeg", "image/webp"}, PerPicture: 0.04, Assumed: true}
}

// PricedPerPicture reports whether m's price is per picture (Imagen's and
// OpenAI's), not by tokens (Gemini's).
func (m Model) PricedPerPicture() bool { return m.Kind == KindImagen || m.Kind == KindOpenAI }

// ProviderOf is the provider whose credentials m's calls use.
func (m Model) ProviderOf() string {
	if m.Provider != "" {
		return m.Provider
	}
	return "gemini"
}

// Models are the catalog's models, in its order (the default first).
func Models() []Model { return slices.Clone(catalog) }

// Lookup is what the model id ("provider/" prefix and all) can do: its
// catalog entry, else its family's most conservative values, marked
// Assumed (PIC-03). ok is false for a model that isn't an image model.
func Lookup(id string) (Model, bool) {
	name := id
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	for _, m := range catalog {
		if m.ID == name {
			return m, true
		}
	}
	switch {
	case strings.HasPrefix(name, "gpt-image-") || strings.HasPrefix(name, "dall-e"):
		m := openAIModel(name, name)
		return m, true
	case strings.HasPrefix(name, "imagen-"):
		return Model{ID: name, Name: name, Kind: KindImagen, MaxPictures: 4, Ratios: ImagenRatios, NegativePrompt: true, People: true, Seed: true,
			Formats: []string{"image/png", "image/jpeg"}, PerPicture: 0.04, Assumed: true}, true
	case strings.Contains(name, "image") || strings.Contains(name, "nano-banana"):
		return Model{ID: name, Name: name, Kind: KindGemini, MaxReferences: 3, MaxPictures: 1, Ratios: StandardRatios,
			Formats: []string{"image/png"}, SizeTokens: map[string]int{"": 1290}, Assumed: true}, true
	}
	return Model{}, false
}

// Reference is a picture a request starts from, with its role.
type Reference struct {
	Role string
	MIME string
	Size int
}

// Request is what a Pictures turn asks for: what Check takes.
type Request struct {
	Prompt       string
	References   []Reference
	Count        int
	AspectRatio  string
	Size         string
	Thinking     string
	Search       bool
	SearchImages bool
	Avoid        string
	People       string
	Seed         bool
	Enhance      bool
	Format       string
	Quality      int
}

// People settings (PIC-29).
var PeopleSettings = []string{"DONT_ALLOW", "ALLOW_ADULT", "ALLOW_ALL"}

// Check says what in r m doesn't take, each with what it does (nil: all
// of it): the service's check before anything is sent (PIC-62).
func (m Model) Check(r Request) []string {
	var out []string
	no := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	if strings.TrimSpace(r.Prompt) == "" {
		no("say what the picture shows")
	}
	if m.Kind == KindImagen && EstimateTokens(r.Prompt) > ImagenPromptTokens {
		no("the prompt is longer than %s takes (about %d tokens; at most %d)", m.Name, EstimateTokens(r.Prompt), ImagenPromptTokens)
	}
	if n := len(r.References); n > m.MaxReferences {
		if m.MaxReferences == 0 {
			no("%s doesn't start from pictures", m.Name)
		} else {
			no("%s takes at most %d reference pictures (%d given)", m.Name, m.MaxReferences, n)
		}
	}
	roles := map[string]int{}
	for i, ref := range r.References {
		roles[ref.Role]++
		switch {
		case ref.Role != "" && ref.Role != RoleCharacter && ref.Role != RoleObject && ref.Role != RoleStyle:
			no("reference %d: %q isn't a role (character, object or style)", i+1, ref.Role)
		case !slices.Contains(ReferenceTypes, ref.MIME):
			no("reference %d is %s: PNG, JPEG or WebP only", i+1, ref.MIME)
		case ref.Size > MaxReferenceBytes:
			no("reference %d is larger than %d MB", i+1, MaxReferenceBytes>>20)
		}
	}
	if m.MaxCharacters > 0 && roles[RoleCharacter] > m.MaxCharacters {
		no("%s takes at most %d character references", m.Name, m.MaxCharacters)
	}
	if m.MaxObjects > 0 && roles[RoleObject] > m.MaxObjects {
		no("%s takes at most %d object references", m.Name, m.MaxObjects)
	}
	if r.Count > max(m.MaxPictures, 1) {
		no("%s makes at most %d picture(s) a turn", m.Name, max(m.MaxPictures, 1))
	}
	if r.AspectRatio != "" && !slices.Contains(m.Ratios, r.AspectRatio) {
		no("%s doesn't make %s pictures (%s)", m.Name, r.AspectRatio, strings.Join(m.Ratios, ", "))
	}
	if r.Size != "" && !slices.Contains(m.Sizes, r.Size) {
		if len(m.Sizes) == 0 {
			no("%s has one size", m.Name)
		} else {
			no("%s doesn't make %s pictures (%s)", m.Name, r.Size, strings.Join(m.Sizes, ", "))
		}
	}
	if r.Thinking != "" && !slices.Contains(m.Thinking, r.Thinking) {
		no("%s doesn't take the thinking level %s", m.Name, r.Thinking)
	}
	if r.Search && m.Search == "" {
		no("%s doesn't search the web", m.Name)
	}
	if r.SearchImages && m.Search != "image" {
		no("%s doesn't search images", m.Name)
	}
	if r.Avoid != "" && !m.NegativePrompt {
		no("%s takes what to avoid in the prompt itself", m.Name)
	}
	if r.People != "" && (!m.People || !slices.Contains(PeopleSettings, r.People)) {
		no("%s doesn't take the people setting %s", m.Name, r.People)
	}
	if r.Seed && !m.Seed {
		no("%s doesn't take a seed", m.Name)
	}
	if r.Enhance && !m.Enhance {
		no("%s doesn't improve prompts", m.Name)
	}
	if r.Format != "" && !slices.Contains(m.Formats, r.Format) {
		no("%s doesn't make %s pictures", m.Name, r.Format)
	}
	if r.Quality < 0 || r.Quality > 100 {
		no("quality is 0 to 100")
	}
	return out
}

// EstimateTokens is about how many tokens text is (four characters each).
func EstimateTokens(text string) int { return (len(text) + 3) / 4 }

// OutputTokens are about what one picture of size costs in output tokens
// (Gemini; 0 when the catalog doesn't say).
func (m Model) OutputTokens(size string) int {
	if n, ok := m.SizeTokens[size]; ok {
		return n
	}
	if n, ok := m.SizeTokens["1K"]; ok {
		return n
	}
	return m.SizeTokens[""]
}
