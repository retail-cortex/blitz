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
	"errors"
	"fmt"
	"slices"
	"sync"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// voiceService implements VoiceService (spec_063_voice VOX-30, VOX-31):
// one conversation a workspace, its audio and events on Converse's stream,
// the person's audio and what the window shows waiting by SendVoice.
type voiceService struct{ s *Server }

// voices are the open conversations, by ID and by workspace.
type voices struct {
	mu   sync.Mutex
	byID map[string]*voiceConv
}

type voiceConv struct {
	id  string
	dir string
	v   *engine.Voice
}

// ErrUnknownVoice is a conversation that has ended or never was.
var errUnknownVoice = errors.New("no such voice conversation: it has ended")

// voiceError is err with its reason (VOX-31).
func voiceError(err error) error {
	switch {
	case errors.Is(err, engine.ErrVoiceOff):
		return apiError(connect.CodeFailedPrecondition, "VOICE_OFF", err)
	case errors.Is(err, runtime.ErrVoiceNotSetUp):
		return apiError(connect.CodeFailedPrecondition, "VOICE_NOT_SET_UP", err)
	case errors.Is(err, runtime.ErrVoiceModel):
		return apiError(connect.CodeInvalidArgument, "VOICE_MODEL", err)
	case errors.Is(err, errUnknownVoice):
		return apiError(connect.CodeNotFound, "UNKNOWN_VOICE", err)
	}
	return toAPI(err)
}

func (h voiceService) DescribeVoice(ctx context.Context, r req[pb.DescribeVoiceRequest]) (*connect.Response[pb.DescribeVoiceResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	global, err := config.Load(h.s.configDir)
	if err != nil {
		return nil, invalid(err)
	}
	cfg := global
	if dir != "" {
		if wcfg, err := config.LoadWorkspace(h.s.configDir, dir); err == nil {
			cfg = wcfg // the workspace's providers; [voice] stays the person's
			cfg.Voice = global.Voice
		}
	}
	v := global.Voice
	out := &pb.DescribeVoiceResponse{
		Enabled: v.Enabled, Model: v.Model, Voice: v.Voice, Listen: v.ListenMode(), Summaries: v.SummaryMode(),
		IdleMinutes: int32(v.Idle().Minutes()), Voices: config.GeminiVoices,
	}
	if dir != "" {
		h.s.mu.Lock()
		w := h.s.workspaces[dir]
		h.s.mu.Unlock()
		if w != nil {
			out.PersonalityVoice = w.Personality().Voice
		}
	}
	backend, err := runtime.VoiceBackend(ctx, cfg)
	if err != nil {
		out.NotSetUp = err.Error()
		return ok(out)
	}
	out.Backend = backend
	out.ModelInUse = v.Model
	if r.Msg.ListModels {
		models, def, err := runtime.VoiceModels(ctx, cfg)
		if err == nil {
			out.Models = models
		}
		if out.ModelInUse == "" {
			out.ModelInUse = "gemini/" + def
		}
	}
	return ok(out)
}

func (h voiceService) Converse(ctx context.Context, r req[pb.ConverseRequest], stream *connect.ServerStream[pb.ConverseResponse]) error {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return err
	}
	dir := w.Dir()
	vs := &h.s.voice
	vs.mu.Lock()
	if vs.byID == nil {
		vs.byID = map[string]*voiceConv{}
	}
	for _, c := range vs.byID {
		if c.dir == dir {
			vs.mu.Unlock()
			return apiError(connect.CodeFailedPrecondition, "VOICE_BUSY", errors.New("a voice conversation is already open in this workspace"))
		}
	}
	conv := &voiceConv{id: newID(), dir: dir}
	vs.byID[conv.id] = conv // held while it starts, so a second waits its turn
	vs.mu.Unlock()
	defer func() {
		vs.mu.Lock()
		delete(vs.byID, conv.id)
		vs.mu.Unlock()
	}()

	// Events go out in order, from one goroutine: the stream is the
	// handler's alone.
	events := make(chan *pb.ConverseResponse, 512)
	var mu sync.Mutex
	closed := false
	push := func(m *pb.ConverseResponse) {
		mu.Lock()
		defer mu.Unlock()
		if closed {
			return
		}
		select {
		case events <- m:
		default: // a window that stopped reading loses audio, not the service
		}
	}
	defer func() {
		mu.Lock()
		closed = true
		mu.Unlock()
	}()
	if r.Msg.SampleVoice != "" && !slices.Contains(config.GeminiVoices, r.Msg.SampleVoice) {
		return invalid(fmt.Errorf("%q isn't one of Gemini's voices", r.Msg.SampleVoice))
	}
	v, err := w.StartVoice(ctx, engine.VoiceOptions{SessionID: r.Msg.SessionId, SummaryOnly: r.Msg.SummaryOnly, SampleVoice: r.Msg.SampleVoice, OnEvent: func(e engine.VoiceEvent) {
		if m := voiceEventProto(e); m != nil {
			push(m)
		}
	}})
	if err != nil {
		return voiceError(err)
	}
	conv.v = v
	defer v.Close()
	cfg := w.VoiceSettings()
	if err := stream.Send(&pb.ConverseResponse{Started: &pb.VoiceStarted{Id: conv.id, Model: v.ModelID(), Voice: v.VoiceName(), Listen: cfg.ListenMode()}}); err != nil {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case m := <-events:
			if err := stream.Send(m); err != nil {
				return nil
			}
			if m.Ended != "" {
				return nil
			}
		}
	}
}

// voiceEventProto is e as the stream sends it.
func voiceEventProto(e engine.VoiceEvent) *pb.ConverseResponse {
	switch e.Kind {
	case "audio":
		return &pb.ConverseResponse{Audio: e.Audio}
	case "heard":
		return &pb.ConverseResponse{Heard: &pb.VoiceText{Text: e.Text, Final: e.Final}}
	case "said":
		return &pb.ConverseResponse{Said: &pb.VoiceText{Text: e.Text, Final: e.Final}}
	case "state":
		return &pb.ConverseResponse{State: e.State}
	case "interrupted":
		return &pb.ConverseResponse{Interrupted: true}
	case "run_task":
		return &pb.ConverseResponse{RunTask: e.Text}
	case "steer":
		return &pb.ConverseResponse{Steer: e.Text}
	case "stop":
		return &pb.ConverseResponse{Stop: true}
	case "answer":
		return &pb.ConverseResponse{Answer: e.Text}
	case "approve":
		return &pb.ConverseResponse{Approve: &pb.VoiceApproval{Id: e.ID, Decision: e.Decision}}
	case "error":
		return &pb.ConverseResponse{Error: e.Text}
	case "end":
		return &pb.ConverseResponse{Ended: e.Text}
	}
	return nil
}

func (h voiceService) SendVoice(_ context.Context, r req[pb.SendVoiceRequest]) (*connect.Response[pb.SendVoiceResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	vs := &h.s.voice
	vs.mu.Lock()
	conv := vs.byID[r.Msg.Id]
	vs.mu.Unlock()
	if conv == nil || conv.v == nil || conv.dir != dir {
		return nil, voiceError(errUnknownVoice)
	}
	v := conv.v
	switch {
	case r.Msg.End:
		v.Close()
	case r.Msg.Speaking != nil:
		err = v.Activity(*r.Msg.Speaking)
	case r.Msg.Waiting != nil:
		wt := r.Msg.Waiting
		v.Waiting(engine.VoiceWaiting{ApprovalID: wt.ApprovalId, Approval: wt.Approval, Question: wt.Question, Options: wt.Options})
	case len(r.Msg.Audio) > 0:
		err = v.Audio(r.Msg.Audio)
	}
	if err != nil {
		return nil, voiceError(err)
	}
	return ok(&pb.SendVoiceResponse{})
}

// endVoices ends every conversation when voice is turned off (VOX-02).
func (s *Server) endVoices() {
	s.voice.mu.Lock()
	var open []*engine.Voice
	for _, c := range s.voice.byID {
		if c.v != nil {
			open = append(open, c.v)
		}
	}
	s.voice.mu.Unlock()
	for _, v := range open {
		v.End("off")
	}
}
