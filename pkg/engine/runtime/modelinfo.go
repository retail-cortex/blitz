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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/retail-cortex/blitz/pkg/modelfacts"
	"google.golang.org/adk/v2/model"
)

// One record per model (spec_056_conversation_adapters §4): what it reads,
// whether it calls tools and how it thinks, how much it holds, what it
// costs. Every capability decision reads it; it's built at each call from
// Blitz's table, what the provider says of the model, the organization's
// catalog, the settings' prices and what Blitz has learned from the
// model's own refusals (CA-11, CA-12), so a change applies without a
// restart.

// ToolMode is how a model calls tools.
type ToolMode int

const (
	// ToolsNative: the API's own function calls.
	ToolsNative ToolMode = iota
	// ToolsNone: the model doesn't call tools; its agent runs without
	// them (CA-103).
	ToolsNone
)

// ThinkingMode is how a model reasons.
type ThinkingMode int

const (
	// ThinkingNone: no reasoning setting applies.
	ThinkingNone ThinkingMode = iota
	// ThinkingBudget: a token budget turns reasoning on and sizes it.
	ThinkingBudget
	// ThinkingAdaptive: the model decides, at an effort; a budget is refused.
	ThinkingAdaptive
	// ThinkingAlways: adaptive, and it can't be turned off.
	ThinkingAlways
)

// mediaFamily is which provider's rules a model's attachments follow.
type mediaFamily int

const (
	mediaOther mediaFamily = iota
	mediaGemini
	mediaClaude
)

// Facts a record can hold as guesses (CA-11): Blitz's table doesn't know
// the model, so they're the wire API's usual, corrected by CA-13.
const (
	factContext  = "context_window"
	factOutput   = "max_output"
	factTools    = "tools"
	factVision   = "vision"
	factThinking = "thinking"
)

// ModelInfo is what Blitz knows of one model (CA-10).
type ModelInfo struct {
	// API is the wire API (gemini, anthropic, openai), Provider the
	// provider as configured, Name the model's canonical name.
	API, Provider, Name string
	// ContextWindow and MaxOutput are in tokens.
	ContextWindow, MaxOutput int
	// Tools is how it calls tools.
	Tools ToolMode
	// Vision is whether it reads images.
	Vision bool
	// Thinking is how it reasons; Effort whether it takes an effort level;
	// Sampling whether it takes temperature and top_p.
	Thinking ThinkingMode
	Effort   bool
	Sampling bool
	// Streams is whether it streams.
	Streams bool
	// Price is what it costs; nil when Blitz has no price for it.
	Price *config.ModelPrice
	// Refused are the settings it refuses (learned, CA-13).
	Refused []string
	// Guesses are the facts above that are guesses, not known (CA-11).
	Guesses []string
	// Learned is what Blitz learned from its refusals (CA-13).
	Learned []modelfacts.Fact

	media mediaFamily
}

// Accepts is what the model takes as attachments (CA-60, CA-61), upload
// saying whether files can go through the provider's Files API; a model
// that doesn't read images gets each as a note (CA-64).
func (m ModelInfo) Accepts(upload bool) []Accept {
	text := Accept{Kind: images.KindText, MIMEs: plainText, MaxBytes: images.MaxTextBytes, AsText: true}
	var list []Accept
	switch m.media {
	case mediaGemini:
		media := Accept{MaxBytes: geminiInline, Inline: geminiInline}
		if upload {
			media.MaxBytes, media.Upload = images.MaxMediaBytes, true
		}
		audio, video := media, media
		audio.Kind, audio.MIMEs = images.KindAudio, mimesOf(images.KindAudio)
		video.Kind, video.MIMEs = images.KindVideo, mimesOf(images.KindVideo)
		list = []Accept{
			{Kind: images.KindImage, MIMEs: geminiImages, MaxBytes: 20 << 20, Inline: 20 << 20},
			{Kind: images.KindDocument, MIMEs: pdf, MaxBytes: images.MaxDocumentBytes, Inline: geminiInline, Pages: 1000, AsText: true},
			text, audio, video,
		}
	case mediaClaude:
		list = []Accept{
			{Kind: images.KindImage, MIMEs: commonImages, MaxBytes: 20 << 20, Inline: 20 << 20},
			{Kind: images.KindDocument, MIMEs: pdf, MaxBytes: images.MaxDocumentBytes, Inline: anthropicBytes, Pages: 100, AsText: true},
			text,
		}
	default:
		list = []Accept{
			{Kind: images.KindImage, MIMEs: commonImages, MaxBytes: 20 << 20, Inline: 20 << 20},
			{Kind: images.KindDocument, MIMEs: pdf, MaxBytes: images.MaxDocumentBytes, AsText: true},
			text,
		}
	}
	if !m.Vision {
		list = slices.DeleteFunc(list, func(a Accept) bool { return a.Kind == images.KindImage })
	}
	return list
}

// Guess reports whether fact is a guess.
func (m ModelInfo) Guess(fact string) bool { return slices.Contains(m.Guesses, fact) }

// refuses reports whether the model refuses setting (learned).
func (m ModelInfo) refuses(setting string) bool { return slices.Contains(m.Refused, setting) }

// CompactionPoint is where a conversation on the model is compacted
// (CA-110): token_threshold when it's set to other than its default,
// else the model's context window less its maximum output, less 10%.
// A model whose window is a guess keeps the setting.
func (m ModelInfo) CompactionPoint(configured int) int {
	if configured != config.DefaultTokenThreshold || m.Guess(factContext) || m.ContextWindow <= 0 {
		return configured
	}
	room := m.ContextWindow - m.MaxOutput
	if room <= 0 {
		room = m.ContextWindow / 2
	}
	return room - room/10
}

// canonicalModel is the name a model is known by in Blitz's table,
// whatever the provider calls it: lower case, without Gemini's
// "models/", Bedrock's region and vendor prefixes and version suffix
// ("us.anthropic.claude-opus-4-1-20250805-v1:0"), Vertex AI's publisher
// path and "@version" ("claude-opus-4-1@20250805").
func canonicalModel(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.TrimPrefix(n, "models/")
	if i := strings.LastIndex(n, "/models/"); i >= 0 {
		n = n[i+len("/models/"):]
	}
	n = bedrockRegion.ReplaceAllString(n, "")
	n = strings.TrimPrefix(n, "anthropic.")
	n = bedrockVersion.ReplaceAllString(n, "")
	if i := strings.IndexByte(n, '@'); i > 0 {
		n = n[:i]
	}
	return n
}

var (
	bedrockRegion  = regexp.MustCompile(`^(us|eu|apac|global|jp|au|us-gov|ca)\.`)
	bedrockVersion = regexp.MustCompile(`-v\d+(:\d+)?$`)
)

// bedrockID reports whether name is one of Bedrock's model IDs.
func bedrockID(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(bedrockRegion.ReplaceAllString(n, ""), "anthropic.")
}

// wireAPI is the wire API a configured provider's models usually speak (a
// model built knows its own: Info's api).
func wireAPI(provider string) string {
	switch provider {
	case "gemini", "vertex":
		return "gemini"
	case "anthropic", "vertex-anthropic", "bedrock", "azure-foundry":
		return "anthropic"
	}
	return "openai"
}

// registry holds what's known of models beyond Blitz's table: what
// providers and the organization say, the settings' prices, and the
// learned facts' store.
type registry struct {
	mu      sync.Mutex
	listed  map[string]listedFacts       // by modelfacts.Key
	catalog map[string]enterprise.Model  // the organization's, by model ID
	pricing map[string]config.ModelPrice // the settings' (with Blitz's defaults)
	store   *modelfacts.Store
	told    map[string]bool // facts told to the person, once each
}

// listedFacts are what a provider's own listing says of a model (Ollama's
// /api/show); zero values say nothing.
type listedFacts struct {
	ContextWindow int
	Tools, Vision *bool
}

var models = &registry{listed: map[string]listedFacts{}, catalog: map[string]enterprise.Model{}, told: map[string]bool{}}

// SetLearnedStore is where learned facts are kept (the engine sets
// ~/.blitz/models.learned.toml; nil keeps none).
func SetLearnedStore(s *modelfacts.Store) {
	models.mu.Lock()
	defer models.mu.Unlock()
	models.store = s
}

func learnedStore() *modelfacts.Store {
	models.mu.Lock()
	defer models.mu.Unlock()
	return models.store
}

// setPricing is the settings' prices, read for every record.
func setPricing(p map[string]config.ModelPrice) {
	models.mu.Lock()
	defer models.mu.Unlock()
	models.pricing = p
}

// setListed records what provider says of model.
func setListed(provider, model string, f listedFacts) {
	models.mu.Lock()
	defer models.mu.Unlock()
	models.listed[modelfacts.Key(provider, canonicalModel(model))] = f
}

// setCatalog records the organization's description of its model.
func setCatalog(m enterprise.Model) {
	models.mu.Lock()
	defer models.mu.Unlock()
	models.catalog[strings.ToLower(m.ID)] = m
}

// Info is what's known of provider's model now, reached through api (the
// wire API: gemini, anthropic, openai; "" for provider's usual) (CA-10,
// CA-11): Blitz's table, then the provider's listing, what was learned,
// the organization's catalog (for its models), and the settings' price,
// each later one winning.
func Info(api, provider, model string) ModelInfo {
	name := canonicalModel(model)
	if api == "" {
		api = wireAPI(provider)
	}
	m := builtinInfo(api, name)
	m.API, m.Provider, m.Name = api, provider, name
	if m.media == mediaClaude && (provider == "bedrock" || bedrockID(model)) {
		m.media = mediaOther // Claude on Bedrock gets a PDF's text (spec_011_images IMG-51)
	}

	models.mu.Lock()
	listed, hasListed := models.listed[modelfacts.Key(provider, name)]
	cat, hasCat := models.catalog[strings.ToLower(model)]
	pricing := models.pricing
	store := models.store
	models.mu.Unlock()

	if hasListed {
		if listed.ContextWindow > 0 {
			m.ContextWindow = listed.ContextWindow
			m.Guesses = without(m.Guesses, factContext)
		}
		if listed.Tools != nil {
			m.Tools = ToolsNative
			if !*listed.Tools {
				m.Tools = ToolsNone
			}
			m.Guesses = without(m.Guesses, factTools)
		}
		if listed.Vision != nil {
			m.Vision = *listed.Vision
			m.Guesses = without(m.Guesses, factVision)
		}
	}
	applyLearned(&m, store.Facts(provider, name, time.Now()))
	if hasCat { // what the organization says wins over what was learned (CA-14)
		if caps, ok := cat.Caps(); ok {
			m.Vision = caps.Images
			m.Tools = ToolsNative
			if !caps.Tools {
				m.Tools = ToolsNone
			}
			if !caps.Thinking {
				m.Thinking = ThinkingNone
			} else if m.Thinking == ThinkingNone {
				m.Thinking = ThinkingAdaptive
			}
			if caps.Context > 0 {
				m.ContextWindow = caps.Context
			}
			m.Guesses = without(m.Guesses, factContext, factTools, factVision, factThinking)
		}
		if cat.MaxOutputTokens > 0 {
			m.MaxOutput = cat.MaxOutputTokens
			m.Guesses = without(m.Guesses, factOutput)
		}
		if cat.Price != nil {
			m.Price = &config.ModelPrice{InputPerMTok: cat.Price.InputPerMTok, OutputPerMTok: cat.Price.OutputPerMTok}
		}
	}
	if m.Price == nil && pricing != nil {
		if p, ok := config.PriceFor(pricing, name); ok {
			m.Price = &p
		}
	}
	return m
}

// applyLearned overlays what was learned of the model on m (CA-13).
func applyLearned(m *ModelInfo, facts []modelfacts.Fact) {
	for _, f := range facts {
		m.Learned = append(m.Learned, f)
		switch f.Kind {
		case modelfacts.NoTools:
			m.Tools = ToolsNone
			m.Guesses = without(m.Guesses, factTools)
		case modelfacts.NoImages:
			m.Vision = false
			m.Guesses = without(m.Guesses, factVision)
		case modelfacts.NoStream:
			m.Streams = false
		case modelfacts.NoThinking:
			m.Thinking, m.Effort = ThinkingNone, false
		case modelfacts.NoParam:
			m.Refused = append(m.Refused, f.Value)
		case modelfacts.ContextWindow:
			if n, err := strconv.Atoi(f.Value); err == nil && n > 0 {
				m.ContextWindow = n
				m.Guesses = without(m.Guesses, factContext)
			}
		case modelfacts.MaxOutput:
			if n, err := strconv.Atoi(f.Value); err == nil && n > 0 {
				m.MaxOutput = n
				m.Guesses = without(m.Guesses, factOutput)
			}
		}
	}
}

// infoOf is the record of the model m is (a fallback chain's primary's),
// when Blitz built it.
func infoOf(m model.LLM) (ModelInfo, bool) {
	switch m := m.(type) {
	case *imageModel:
		return infoOf(m.inner)
	case *fallbackModel:
		return infoOf(m.chain[0])
	case *settingsModel:
		return m.info(), true
	}
	return ModelInfo{}, false
}

func without(list []string, drop ...string) []string {
	return slices.DeleteFunc(list, func(s string) bool { return slices.Contains(drop, s) })
}

var openAIReasoning = regexp.MustCompile(`^o[134](-|$)`)

// unknownWindow is the context window guessed for a model nothing
// describes (CA-11).
const unknownWindow = 32_000

// builtinInfo is Blitz's table: what's known of the families it ships
// for, and a wire API's usual record, marked as guesses, for the rest.
func builtinInfo(api, name string) ModelInfo {
	m := ModelInfo{Tools: ToolsNative, Streams: true, Vision: true, Sampling: true}
	switch {
	case strings.HasPrefix(name, "claude-"):
		if api == "anthropic" { // Claude's rules on attachments are its Messages API's
			m.media = mediaClaude
		}
		m.ContextWindow, m.MaxOutput = 200_000, 64_000
		if strings.HasPrefix(name, "claude-3") {
			m.MaxOutput = 8_192
		}
		m.Sampling, m.Effort = supportsSampling(name), supportsEffort(name)
		switch {
		case alwaysThinks(name):
			m.Thinking = ThinkingAlways
		case adaptiveThinking(name):
			m.Thinking = ThinkingAdaptive
		case supportsThinking(name):
			m.Thinking = ThinkingBudget
		}
	case api == "gemini" || strings.HasPrefix(name, "gemini"):
		if api == "gemini" {
			m.media = mediaGemini
		}
		m.ContextWindow, m.MaxOutput = 1_048_576, 65_536
		m.Thinking, m.Effort = ThinkingBudget, !strings.HasPrefix(name, "gemini-2")
		if strings.HasPrefix(name, "gemini-1") || strings.HasPrefix(name, "gemini-2.0") {
			m.Thinking, m.MaxOutput = ThinkingNone, 8_192
		}
	case strings.HasPrefix(name, "gpt-5"):
		m.ContextWindow, m.MaxOutput = 400_000, 128_000
		m.Thinking, m.Effort, m.Sampling = ThinkingAdaptive, true, false
	case strings.HasPrefix(name, "gpt-4.1"):
		m.ContextWindow, m.MaxOutput = 1_047_576, 32_768
	case strings.HasPrefix(name, "gpt-4o"):
		m.ContextWindow, m.MaxOutput = 128_000, 16_384
	case openAIReasoning.MatchString(name):
		m.ContextWindow, m.MaxOutput = 200_000, 100_000
		m.Thinking, m.Effort, m.Sampling = ThinkingAdaptive, true, false
	default:
		// Not in the table: the wire API's usual, each fact a guess.
		m.ContextWindow, m.MaxOutput = unknownWindow, 8_192
		m.Guesses = []string{factContext, factOutput, factTools, factVision}
	}
	return m
}
