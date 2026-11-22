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
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/genai"
)

// The voice's model (spec_063_voice VOX-05 to VOX-07): a Gemini Live model
// through Blitz's gemini provider, its API key or Vertex AI, run by ADK's
// live runner (the model's genai client opens the Live API's socket).

// ErrVoiceNotSetUp is voice with no Google provider to use (VOX-03).
var ErrVoiceNotSetUp = errors.New("voice needs Google's Gemini: set up Gemini in Settings › Providers (an API key, or Vertex AI)")

// ErrVoiceModel is a [voice] model that isn't a Gemini Live model.
var ErrVoiceModel = errors.New("[voice] model isn't a Gemini Live model")

// ErrNoLiveModel is a provider none of whose live models answers for this
// key or project (VOX-06a).
var ErrNoLiveModel = errors.New("no Gemini Live model answers for this Gemini key or Vertex AI project")

// VoiceModel is the voice's model and how it's reached.
type VoiceModel struct {
	model.LLM
	// ID is the model's name ("gemini-…"), Backend the API it's on.
	ID      string
	Backend genai.Backend
}

// liveClientConfig is cc ready for the Live API, which needs its API
// version named.
func liveClientConfig(cc *genai.ClientConfig) *genai.ClientConfig {
	if cc.HTTPOptions.APIVersion == "" {
		cc.HTTPOptions.APIVersion = "v1beta"
		if cc.Backend == genai.BackendVertexAI {
			cc.HTTPOptions.APIVersion = "v1beta1"
		}
	}
	return cc
}

// NewVoiceModel builds the voice's model from cfg (VOX-05, VOX-06,
// VOX-06a): [voice] model ("gemini/<model>"), checked to answer, else the
// best live model that answers for this key or project (ErrNoLiveModel
// when none does). ErrVoiceNotSetUp when Gemini can't sign in.
func NewVoiceModel(ctx context.Context, cfg *config.Config) (*VoiceModel, error) {
	// Voice is Gemini's live model: a provider of the person's own
	// (runtime#6).
	if err := ownProviderOff(cfg, "gemini"); err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrVoiceNotSetUp, err)
	}
	cc, err := geminiClientConfig(ctx, cfg.LLM.Gemini, policyFrom(cfg.LLM))
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrVoiceNotSetUp, err)
	}
	cc = liveClientConfig(cc)
	name := ""
	if ref := strings.TrimSpace(cfg.Voice.Model); ref != "" {
		provider, n := ParseModelRef(ref, "gemini")
		if provider != "gemini" || n == "" {
			return nil, fmt.Errorf("%w: %q", ErrVoiceModel, ref)
		}
		name = n
	}
	voice := cmp.Or(cfg.Voice.Voice, config.DefaultVoice)
	if name == "" {
		if name, err = defaultVoiceModel(ctx, cc, voice); err != nil {
			return nil, err
		}
	} else if err := probeLive(ctx, cc, name, liveSetup(voice)); err != nil { // said now, not mid-conversation
		return nil, fmt.Errorf("the voice model %s doesn't answer: %w", name, err)
	}
	m, err := gemini.NewModel(ctx, name, cc)
	if err != nil {
		return nil, err
	}
	return &VoiceModel{LLM: m, ID: name, Backend: cc.Backend}, nil
}

// VoiceBackend is how voice would reach Gemini ("gemini" for the Gemini
// API, "vertex" for Vertex AI), or why it can't (ErrVoiceNotSetUp,
// VOX-03). It asks nothing of Google.
func VoiceBackend(ctx context.Context, cfg *config.Config) (string, error) {
	if err := ownProviderOff(cfg, "gemini"); err != nil {
		return "", fmt.Errorf("%w (%v)", ErrVoiceNotSetUp, err)
	}
	cc, err := geminiClientConfig(ctx, cfg.LLM.Gemini, policyFrom(cfg.LLM))
	if err != nil {
		return "", fmt.Errorf("%w (%v)", ErrVoiceNotSetUp, err)
	}
	if cc.Backend == genai.BackendVertexAI {
		return "vertex", nil
	}
	return "gemini", nil
}

// VoiceModels are the live models that answer for this key or project,
// best first (VOX-41, VOX-06a), and the one a conversation would use; none
// answering is ErrNoLiveModel, with the provider's reason.
func VoiceModels(ctx context.Context, cfg *config.Config) ([]string, string, error) {
	cc, err := geminiClientConfig(ctx, cfg.LLM.Gemini, policyFrom(cfg.LLM))
	if err != nil {
		return nil, "", fmt.Errorf("%w (%v)", ErrVoiceNotSetUp, err)
	}
	cc = liveClientConfig(cc)
	models, err := answeringLive(ctx, cc, cmp.Or(cfg.Voice.Voice, config.DefaultVoice))
	if err != nil {
		return nil, "", err
	}
	return models, models[0], nil
}

// defaultVoiceModel is the best live model that answers (VOX-06a).
func defaultVoiceModel(ctx context.Context, cc *genai.ClientConfig, voice string) (string, error) {
	models, err := answeringLive(ctx, cc, voice)
	if err != nil {
		return "", err
	}
	return models[0], nil
}

// How long a live model's check may take, how many run at once, and how
// long their answers are kept (variables for tests).
var (
	liveProbeTimeout = 8 * time.Second
	liveProbeAtOnce  = 4
	liveKeep         = 10 * time.Minute
)

// liveFound is a provider's answering live models, kept a while.
type liveFound struct {
	models []string
	err    error
	at     time.Time
}

var (
	liveMu    sync.Mutex
	liveCache = map[string]liveFound{}
)

// liveKey is what the answering models depend on: the backend, the
// project and region, the key (hashed) and the voice.
func liveKey(cc *genai.ClientConfig, voice string) string {
	sum := sha256.Sum256([]byte(cc.APIKey))
	return fmt.Sprintf("%v|%s|%s|%x|%s", cc.Backend, cc.Project, cc.Location, sum[:8], voice)
}

// answeringLive is the provider's live models that answer, best first
// (liveBetter): listed, then each one checked (probeLive), as a listing
// names models a key can't use (allowlisted previews, a region's absent
// ones). Kept for liveKeep.
func answeringLive(ctx context.Context, cc *genai.ClientConfig, voice string) ([]string, error) {
	key := liveKey(cc, voice)
	liveMu.Lock()
	if f, ok := liveCache[key]; ok && time.Since(f.at) < liveKeep {
		liveMu.Unlock()
		return f.models, f.err
	}
	liveMu.Unlock()
	listed, err := liveCandidates(ctx, cc)
	if err == nil && len(listed) == 0 {
		err = fmt.Errorf("%w: the provider lists no live models", ErrNoLiveModel)
	}
	var models []string
	if err == nil {
		models, err = probeAll(ctx, cc, listed, liveSetup(voice))
	}
	liveMu.Lock()
	liveCache[key] = liveFound{models: models, err: err, at: time.Now()}
	liveMu.Unlock()
	return models, err
}

// probeAll checks each listed model, liveProbeAtOnce at a time: those that
// answer, best first, or ErrNoLiveModel with the first one's reason.
func probeAll(ctx context.Context, cc *genai.ClientConfig, listed []string, setup *genai.LiveConnectConfig) ([]string, error) {
	type result struct {
		model string
		err   error
	}
	results := make([]result, len(listed))
	sem := make(chan struct{}, liveProbeAtOnce)
	var wg sync.WaitGroup
	for i, m := range listed {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			pctx, cancel := context.WithTimeout(ctx, liveProbeTimeout)
			defer cancel()
			results[i] = result{m, probeLive(pctx, cc, m, setup)}
		}()
	}
	wg.Wait()
	var ok []string
	var first error
	for _, r := range results {
		if r.err == nil {
			ok = append(ok, r.model)
		} else if first == nil {
			first = fmt.Errorf("%s: %w", r.model, r.err)
		}
	}
	if len(ok) == 0 {
		return nil, fmt.Errorf("%w (%v)", ErrNoLiveModel, first)
	}
	slices.SortFunc(ok, liveBetter)
	return ok, nil
}

// liveCandidates are the live models the provider lists: on the Gemini
// API, those it says take bidiGenerateContent; on Vertex AI, which lists
// no actions, Google's models whose name says live or native audio.
func liveCandidates(ctx context.Context, cc *genai.ClientConfig) ([]string, error) {
	client, err := genai.NewClient(ctx, cc)
	if err != nil {
		return nil, err
	}
	var out []string
	base := true
	page, err := client.Models.List(ctx, &genai.ListModelsConfig{QueryBase: &base, PageSize: 100})
	for {
		if err != nil {
			if errors.Is(err, genai.ErrPageDone) {
				break
			}
			return nil, fmt.Errorf("listing the models: %w", err)
		}
		for _, m := range page.Items {
			id := m.Name[strings.LastIndex(m.Name, "/")+1:]
			switch {
			case cc.Backend == genai.BackendVertexAI && (strings.Contains(id, "live") || strings.Contains(id, "native-audio")):
				out = append(out, id)
			case slices.Contains(m.SupportedActions, "bidiGenerateContent"):
				out = append(out, id)
			}
		}
		if page.NextPageToken == "" {
			break
		}
		page, err = page.Next(ctx)
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// liveSetup is the live settings a conversation opens with (VoiceLiveConfig:
// audio in voice, both sides transcribed, resumable), for checking a model
// takes them, not just a bare session.
func liveSetup(voice string) *genai.LiveConnectConfig {
	r := VoiceLiveConfig(voice, false)
	return &genai.LiveConnectConfig{
		ResponseModalities: r.ResponseModalities, SpeechConfig: r.SpeechConfig,
		InputAudioTranscription: r.InputAudioTranscription, OutputAudioTranscription: r.OutputAudioTranscription,
		SessionResumption: r.SessionResumption,
	}
}

// probeLive opens a live session on model with setup and waits for the
// provider to accept it (setupComplete), then closes it: whether this key
// or project can use the model as a conversation does. Nothing is said,
// so nothing is billed. A variable for tests (UseLiveProbe).
var probeLive = func(ctx context.Context, cc *genai.ClientConfig, model string, setup *genai.LiveConnectConfig) error {
	client, err := genai.NewClient(ctx, cc)
	if err != nil {
		return err
	}
	s, err := client.Live.Connect(ctx, model, setup)
	if err != nil {
		return err
	}
	defer s.Close()
	done := make(chan error, 1)
	go func() {
		msg, err := s.Receive()
		switch {
		case err != nil:
			done <- err
		case msg.SetupComplete == nil:
			done <- errors.New("no setup answer")
		default:
			done <- nil
		}
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("no answer: %w", ctx.Err())
	}
}

var (
	versionRE = regexp.MustCompile(`(\d+)\.(\d+)`)
	dateRE    = regexp.MustCompile(`-(\d{2})-(\d{4})(?:$|-)`)
)

// liveBetter orders live models best first: native audio (the family
// voice is built and tested on), then the higher version (3.8 before 2.5),
// then the newer dated release, then a released one before a preview,
// then by name: by what they are, not by how their names sort
// ("gemini-live-2.5" sorts after "gemini-3.8").
func liveBetter(a, b string) int {
	ver := func(s string) (int, int) {
		m := versionRE.FindStringSubmatch(s)
		if m == nil {
			return 0, 0
		}
		x, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[2])
		return x, y
	}
	date := func(s string) int {
		m := dateRE.FindStringSubmatch(s)
		if m == nil {
			return 0
		}
		mo, _ := strconv.Atoi(m[1])
		yr, _ := strconv.Atoi(m[2])
		return yr*100 + mo
	}
	if an, bn := strings.Contains(a, "native-audio"), strings.Contains(b, "native-audio"); an != bn {
		if an {
			return -1
		}
		return 1
	}
	ax, ay := ver(a)
	bx, by := ver(b)
	if c := cmp.Compare(bx*1000+by, ax*1000+ay); c != 0 {
		return c
	}
	if c := cmp.Compare(date(b), date(a)); c != 0 {
		return c
	}
	if ap, bp := strings.Contains(a, "preview"), strings.Contains(b, "preview"); ap != bp {
		if bp {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

// VoiceLiveConfig is the live connection's settings (VOX-07): audio
// answers in voice, both sides transcribed, Gemini's own activity
// detection unless hold, when the page says when the person speaks, and
// resumption, so a connection the server cycles carries on.
func VoiceLiveConfig(voice string, hold bool) agent.LiveRunConfig {
	cfg := agent.LiveRunConfig{
		ResponseModalities: []genai.Modality{genai.ModalityAudio},
		SpeechConfig: &genai.SpeechConfig{VoiceConfig: &genai.VoiceConfig{
			PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{VoiceName: voice},
		}},
		InputAudioTranscription:  &genai.AudioTranscriptionConfig{},
		OutputAudioTranscription: &genai.AudioTranscriptionConfig{},
		SessionResumption:        &genai.SessionResumptionConfig{},
	}
	if hold {
		cfg.RealtimeInputConfig = &genai.RealtimeInputConfig{AutomaticActivityDetection: &genai.AutomaticActivityDetection{Disabled: true}}
	}
	return cfg
}

// UseLiveProbe replaces the live models' check with answers (nil: the
// model answers) and forgets what was kept, for other packages' tests; the
// func returned puts both back.
func UseLiveProbe(answers func(model string) error) (restore func()) {
	old := probeLive
	probeLive = func(_ context.Context, _ *genai.ClientConfig, model string, _ *genai.LiveConnectConfig) error {
		return answers(model)
	}
	liveMu.Lock()
	liveCache = map[string]liveFound{}
	liveMu.Unlock()
	return func() {
		probeLive = old
		liveMu.Lock()
		liveCache = map[string]liveFound{}
		liveMu.Unlock()
	}
}
