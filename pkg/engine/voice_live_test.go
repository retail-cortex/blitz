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

package engine

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/require"
)

// The paid check (spec_063_voice VOX-64): recorded speech (16 kHz WAV, in
// BLITZ_VOICE_WAV) to the real Gemini Live API through the engine, which
// should hand the request to the window (run_task), say so, and live
// through the tool's reply. Only with BLITZ_LIVE=1 and a Gemini key; it
// costs a little:
//
//	bazel test //pkg/engine:engine_test --test_filter=TestVoiceLive \
//	  --test_env=BLITZ_LIVE=1 --test_env=GEMINI_API_KEY --test_env=BLITZ_VOICE_WAV=/path/ask.wav
func TestVoiceLive(t *testing.T) {
	if os.Getenv("BLITZ_LIVE") != "1" || os.Getenv("GEMINI_API_KEY") == "" || os.Getenv("BLITZ_VOICE_WAV") == "" {
		t.Skip("BLITZ_LIVE=1, GEMINI_API_KEY and BLITZ_VOICE_WAV run it")
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	wav, err := os.ReadFile(os.Getenv("BLITZ_VOICE_WAV"))
	require.NoError(t, err)
	key := os.Getenv("GEMINI_API_KEY")
	w, _ := openTestWith(t, nil)
	t.Setenv("GEMINI_API_KEY", key)
	_, err = config.SetValue(w.cfg.Dir, "", "voice.enabled", "true")
	require.NoError(t, err)
	evs := newVoiceEvents()
	v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "live", OnEvent: evs.on})
	require.NoError(t, err)
	defer v.Close()
	t.Logf("model %s", v.ModelID())

	pcm := wav[44:]
	silence := make([]byte, 3200) // 100 ms
	go func() {
		for i := 0; i < len(pcm); i += 3200 {
			_ = v.Audio(pcm[i:min(i+3200, len(pcm))])
			time.Sleep(100 * time.Millisecond)
		}
		for range 30 {
			_ = v.Audio(silence)
			time.Sleep(100 * time.Millisecond)
		}
	}()
	deadline := time.After(40 * time.Second)
	var ran bool
	var said strings.Builder
	for {
		select {
		case ev := <-evs.in:
			switch ev.Kind {
			case "audio", "state":
			default:
				t.Logf("%s %q final=%v", ev.Kind, ev.Text, ev.Final)
			}
			if ev.Kind == "said" {
				said.WriteString(ev.Text)
			}
			if ev.Kind == "run_task" && !ran {
				ran = true
				// The agent works on it, then the turn ends: the voice says so
				// (VOX-17), through Blitz's own message.
				go func() {
					on := w.factsStart("live", ev.Text, nil)
					on(api.Event{ToolCall: &api.ToolCall{Name: "run_shell_command", Args: map[string]any{"command": "go test ./..."}}})
					on(api.Event{ToolResult: &api.ToolResult{Name: "run_shell_command", Result: map[string]any{"exit_code": 0}}})
					time.Sleep(8 * time.Second)
					w.factsEnd("live", api.TurnResult{Output: "All 212 tests pass. The project is a FastAPI backend using SQLModel with Alembic migrations, and a React front end."}, nil)
				}()
			}
			if ev.Kind == "end" || ev.Kind == "error" {
				t.Fatalf("ended: %s", ev.Text)
			}
		case <-deadline:
			require.True(t, ran, "the voice never handed the request over")
			require.Contains(t, strings.ToLower(said.String()), "212", "the agent's answer was said")
			require.NotContains(t, strings.ToLower(said.String()), "agent", "first person: no agent mentioned")
			return
		}
	}
}

// Hold to talk with the real API (VOX-07): Gemini's own activity detection
// off, the person's start and stop sent, and an answer after the stop.
func TestVoiceLiveHold(t *testing.T) {
	if os.Getenv("BLITZ_LIVE") != "1" || os.Getenv("GEMINI_API_KEY") == "" || os.Getenv("BLITZ_VOICE_WAV") == "" {
		t.Skip("BLITZ_LIVE=1, GEMINI_API_KEY and BLITZ_VOICE_WAV run it")
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	wav, err := os.ReadFile(os.Getenv("BLITZ_VOICE_WAV"))
	require.NoError(t, err)
	key := os.Getenv("GEMINI_API_KEY")
	w, _ := openTestWith(t, nil)
	t.Setenv("GEMINI_API_KEY", key)
	for k, v := range map[string]string{"voice.enabled": "true", "voice.listen": "hold"} {
		_, err = config.SetValue(w.cfg.Dir, "", k, v)
		require.NoError(t, err)
	}
	evs := newVoiceEvents()
	v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "hold", OnEvent: evs.on})
	require.NoError(t, err)
	defer v.Close()
	require.NoError(t, v.Activity(true))
	pcm := wav[44:]
	for i := 0; i < len(pcm); i += 3200 {
		require.NoError(t, v.Audio(pcm[i:min(i+3200, len(pcm))]))
		time.Sleep(100 * time.Millisecond)
	}
	require.NoError(t, v.Activity(false))
	deadline := time.After(30 * time.Second)
	for {
		select {
		case ev := <-evs.in:
			if ev.Kind == "said" || ev.Kind == "run_task" || ev.Kind == "heard" {
				t.Logf("%s %q", ev.Kind, ev.Text)
			}
			if ev.Kind == "run_task" || ev.Kind == "said" && ev.Text != "" {
				return
			}
			if ev.Kind == "end" || ev.Kind == "error" {
				t.Fatalf("ended: %s", ev.Text)
			}
		case <-deadline:
			t.Fatal("no answer after the hold ended")
		}
	}
}

// A permission needed is asked in the first person with the real API
// (VOX-13): "I need permission to …, may I?", in plain words, never the
// SQL itself.
func TestVoiceLivePermission(t *testing.T) {
	if os.Getenv("BLITZ_LIVE") != "1" || os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("BLITZ_LIVE=1 and GEMINI_API_KEY run it")
	}
	key := os.Getenv("GEMINI_API_KEY")
	w, _ := openTestWith(t, nil)
	t.Setenv("GEMINI_API_KEY", key)
	_, err := config.SetValue(w.cfg.Dir, "", "voice.enabled", "true")
	require.NoError(t, err)
	evs := newVoiceEvents()
	v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "perm", OnEvent: evs.on})
	require.NoError(t, err)
	defer v.Close()
	time.Sleep(time.Second)
	v.Waiting(VoiceWaiting{ApprovalID: "ap1", Approval: "run `sqlite3 shop.db \"SELECT id, customer_id, total_cents FROM orders WHERE status = 'open' ORDER BY id;\"`"})
	var said strings.Builder
	deadline := time.After(20 * time.Second)
	for {
		select {
		case ev := <-evs.in:
			if ev.Kind == "said" {
				said.WriteString(ev.Text)
			}
			if ev.Kind == "said" && ev.Final && said.Len() > 0 {
				t.Logf("said %q", said.String())
				s := strings.ToLower(said.String())
				require.Contains(t, s, "permission")
				require.Contains(t, s, "may i")
				require.NotContains(t, s, "agent")
				require.NotContains(t, s, "select")
				require.NotContains(t, s, "order by")
				require.NotContains(t, s, "customer_id")
				require.NotContains(t, s, "recipes", "its own words, not the note's example")
				require.Contains(t, s, "order")
				return
			}
		case <-deadline:
			t.Fatalf("not asked; said %q", said.String())
		}
	}
}
