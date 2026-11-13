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

package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/runtime/wirefake"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/genai"
)

const voiceWait = 5 * time.Second

// serveVoice starts a server whose workspaces' voices talk to a fake Live
// API, and returns its voice and config clients.
func serveVoice(t *testing.T) (pb.VoiceServiceClient, pb.ConfigServiceClient, *wirefake.Live) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/nonexistent")
	t.Chdir(t.TempDir())
	live := wirefake.NewLive(t, "test-key")
	s := New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		return engine.Open(ctx, cfg, engine.Options{
			Model: runtime.NewMockLLM("gemini-3.8-flash"),
			NewVoiceModel: func(ctx context.Context, _ *config.Config) (*runtime.VoiceModel, error) {
				m, err := gemini.NewModel(ctx, "gemini-live-test", &genai.ClientConfig{APIKey: "test-key", Backend: genai.BackendGeminiAPI, HTTPOptions: genai.HTTPOptions{BaseURL: live.URL, APIVersion: "v1beta"}})
				return &runtime.VoiceModel{LLM: m, ID: "gemini-live-test", Backend: genai.BackendGeminiAPI}, err
			},
		})
	})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(func() { srv.Close(); s.Close() })
	return pb.NewVoiceServiceClient(http.DefaultClient, srv.URL), pb.NewConfigServiceClient(http.DefaultClient, srv.URL), live
}

// The voice through the service (spec_063_voice VOX-30, VOX-31, VOX-02):
// described off and not set up; refused while off; once on, one
// conversation a workspace, its audio up by SendVoice and its events down
// the stream; an unknown conversation is refused; turning voice off ends
// it at once.
func TestVoiceService(t *testing.T) {
	voice, cfg, live := serveVoice(t)
	ctx := context.Background()
	dir := t.TempDir()

	desc, err := voice.DescribeVoice(ctx, connect.NewRequest(&pb.DescribeVoiceRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.False(t, desc.Msg.Enabled)
	assert.Contains(t, desc.Msg.NotSetUp, "voice needs Google's Gemini")
	assert.Equal(t, "tap", desc.Msg.Listen)
	assert.Equal(t, "voice", desc.Msg.Summaries)
	assert.Equal(t, int32(5), desc.Msg.IdleMinutes)
	assert.Contains(t, desc.Msg.Voices, "Kore")

	err = streamErr(voice.Converse(ctx, connect.NewRequest(&pb.ConverseRequest{Workspace: dir, SessionId: "s"})))
	assert.Equal(t, "VOICE_OFF", reasonOf(err))

	_, err = cfg.SetConfigValue(ctx, connect.NewRequest(&pb.SetConfigValueRequest{Key: "voice.enabled", Value: "true"}))
	require.NoError(t, err)
	t.Setenv("GEMINI_API_KEY", "a-key")
	desc, err = voice.DescribeVoice(ctx, connect.NewRequest(&pb.DescribeVoiceRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.True(t, desc.Msg.Enabled)
	assert.Empty(t, desc.Msg.NotSetUp)
	assert.Equal(t, "gemini", desc.Msg.Backend)

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := voice.Converse(streamCtx, connect.NewRequest(&pb.ConverseRequest{Workspace: dir, SessionId: "s"}))
	require.NoError(t, err)
	next := func() *pb.ConverseResponse {
		t.Helper()
		got := make(chan *pb.ConverseResponse, 1)
		go func() {
			if stream.Receive() {
				got <- stream.Msg()
			} else {
				got <- nil
			}
		}()
		select {
		case m := <-got:
			require.NotNil(t, m, "the stream ended: %v", stream.Err())
			return m
		case <-time.After(voiceWait):
			t.Fatal("nothing on the stream")
			return nil
		}
	}
	started := next().Started
	require.NotNil(t, started)
	assert.Equal(t, "gemini-live-test", started.Model)
	assert.Equal(t, "Kore", started.Voice)
	assert.Equal(t, "tap", started.Listen)
	live.Opened(voiceWait)

	err = streamErr(voice.Converse(ctx, connect.NewRequest(&pb.ConverseRequest{Workspace: dir, SessionId: "s"})))
	assert.Equal(t, "VOICE_BUSY", reasonOf(err), "one a workspace")

	_, err = voice.SendVoice(ctx, connect.NewRequest(&pb.SendVoiceRequest{Workspace: dir, Id: started.Id, Audio: make([]byte, 640)}))
	require.NoError(t, err)
	live.Next("realtimeInput", voiceWait)
	speaking := true
	_, err = voice.SendVoice(ctx, connect.NewRequest(&pb.SendVoiceRequest{Workspace: dir, Id: started.Id, Speaking: &speaking}))
	require.NoError(t, err)
	_, err = voice.SendVoice(ctx, connect.NewRequest(&pb.SendVoiceRequest{Workspace: dir, Id: started.Id, Waiting: &pb.VoiceWaiting{ApprovalId: "ap1", Approval: "run make"}}))
	require.NoError(t, err)
	assert.Contains(t, fmt.Sprint(live.Next("clientContent", voiceWait)), "I need permission to run make, may I?")

	live.Call("c1", "run_task", map[string]any{"request": "Run the tests."}, voiceWait)
	for m := next(); ; m = next() {
		if m.RunTask != "" {
			assert.Equal(t, "Run the tests.", m.RunTask)
			break
		}
	}
	live.Say("On it.", []byte{1, 2})
	var said, audio bool
	for !said || !audio {
		m := next()
		said = said || m.Said != nil && m.Said.Text == "On it."
		audio = audio || len(m.Audio) > 0
	}

	for name, call := range map[string]func() error{
		"unknown": func() error {
			return unary(voice.SendVoice, &pb.SendVoiceRequest{Workspace: dir, Id: "nope", Audio: []byte{1}})
		},
		"another workspace": func() error {
			return unary(voice.SendVoice, &pb.SendVoiceRequest{Workspace: t.TempDir(), Id: started.Id, Audio: []byte{1}})
		},
	} {
		t.Run(name, func(t *testing.T) { assert.Equal(t, "UNKNOWN_VOICE", reasonOf(call())) })
	}

	_, err = cfg.SetConfigValue(ctx, connect.NewRequest(&pb.SetConfigValueRequest{Key: "voice.enabled", Value: "false"}))
	require.NoError(t, err)
	for m := next(); ; m = next() {
		if m.Ended != "" {
			assert.Equal(t, "off", m.Ended, "off at once")
			break
		}
	}
}

// Each engine event has its place on the stream; an unknown one has none.
func TestVoiceEventProto(t *testing.T) {
	for kind, want := range map[string]*pb.ConverseResponse{
		"audio":       {Audio: []byte{1}},
		"heard":       {Heard: &pb.VoiceText{Text: "t", Final: true}},
		"said":        {Said: &pb.VoiceText{Text: "t", Final: true}},
		"state":       {State: "s"},
		"interrupted": {Interrupted: true},
		"run_task":    {RunTask: "t"},
		"steer":       {Steer: "t"},
		"stop":        {Stop: true},
		"answer":      {Answer: "t"},
		"approve":     {Approve: &pb.VoiceApproval{Id: "i", Decision: "d"}},
		"error":       {Error: "t"},
		"end":         {Ended: "t"},
	} {
		t.Run(kind, func(t *testing.T) {
			got := voiceEventProto(engine.VoiceEvent{Kind: kind, Text: "t", Final: true, State: "s", Audio: []byte{1}, ID: "i", Decision: "d"})
			assert.Equal(t, want.String(), got.String())
		})
	}
	assert.Nil(t, voiceEventProto(engine.VoiceEvent{Kind: "nope"}))
}

// Settings › Voice's description lists the live models and the model in
// use (VOX-41); a sample needs one of Gemini's voices; each error has its
// reason (VOX-31).
func TestVoiceDescribeAndReasons(t *testing.T) {
	voice, cfg, live := serveVoice(t)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"models":[{"name":"models/gemini-live-a","supportedGenerationMethods":["bidiGenerateContent"]},{"name":"models/gemini-x","supportedGenerationMethods":["generateContent"]}]}`)
	}))
	defer srv.Close()
	t.Setenv("GOOGLE_GEMINI_BASE_URL", srv.URL)
	t.Setenv("GEMINI_API_KEY", "a-key")
	_, err := cfg.SetConfigValue(ctx, connect.NewRequest(&pb.SetConfigValueRequest{Key: "voice.enabled", Value: "true"}))
	require.NoError(t, err)

	d, err := voice.DescribeVoice(ctx, connect.NewRequest(&pb.DescribeVoiceRequest{ListModels: true}))
	require.NoError(t, err)
	assert.Equal(t, []string{"gemini-live-a"}, d.Msg.Models)
	assert.Equal(t, "gemini/gemini-live-a", d.Msg.ModelInUse)
	_, err = voice.DescribeVoice(ctx, connect.NewRequest(&pb.DescribeVoiceRequest{Workspace: "relative"}))
	assert.Equal(t, "INVALID_WORKSPACE", reasonOf(err))

	err = streamErr(voice.Converse(ctx, connect.NewRequest(&pb.ConverseRequest{Workspace: t.TempDir(), SampleVoice: "Bob"})))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	sample, err := voice.Converse(ctx, connect.NewRequest(&pb.ConverseRequest{Workspace: t.TempDir(), SampleVoice: "Puck"}))
	require.NoError(t, err)
	require.True(t, sample.Receive())
	assert.Equal(t, "Puck", sample.Msg().Started.Voice)
	live.Opened(voiceWait)
	live.Say("Hi.", []byte{1, 2})
	for sample.Receive() {
		if sample.Msg().Ended != "" {
			assert.Equal(t, "summary", sample.Msg().Ended)
		}
	}

	for want, err := range map[string]error{
		"VOICE_OFF":        engine.ErrVoiceOff,
		"VOICE_NOT_SET_UP": runtime.ErrVoiceNotSetUp,
		"VOICE_MODEL":      runtime.ErrVoiceModel,
		"UNKNOWN_VOICE":    errUnknownVoice,
	} {
		assert.Equal(t, want, reasonOf(voiceError(fmt.Errorf("wrapped: %w", err))))
	}
}
