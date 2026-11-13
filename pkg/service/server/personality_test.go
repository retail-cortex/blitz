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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The personalities through the service (spec_064_personality PER-50):
// listed with the person's default and the open workspace's choice,
// saved, read, tried and deleted; the workspace's choice, set with
// SetConfigValue, applies at once; a built-in can't be deleted.
func TestPersonalityService(t *testing.T) {
	c, s := serve(t, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	cfg := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	dir := t.TempDir()
	_, err := c.workspaces.ListStyles(ctx, connect.NewRequest(&pb.ListStylesRequest{Workspace: dir})) // opens it
	require.NoError(t, err)

	list := func() map[string]*pb.Personality {
		res, err := cfg.ListPersonalities(ctx, connect.NewRequest(&pb.ListPersonalitiesRequest{Workspace: dir}))
		require.NoError(t, err)
		out := map[string]*pb.Personality{}
		for _, p := range res.Msg.Personalities {
			out[p.Name] = p
		}
		return out
	}
	all := list()
	require.Contains(t, all, "mentor")
	assert.True(t, all["default"].PersonDefault)
	assert.True(t, all["default"].WorkspaceChoice)
	assert.Equal(t, "built-in", all["mentor"].Source)
	assert.Empty(t, all["mentor"].Text)

	_, err = cfg.SavePersonality(ctx, connect.NewRequest(&pb.SavePersonalityRequest{Personality: &pb.Personality{Name: "coach", Description: "Cheers", Voice: "Puck", Text: "Cheer them on."}}))
	require.NoError(t, err)
	got, err := cfg.GetPersonality(ctx, connect.NewRequest(&pb.GetPersonalityRequest{Name: "coach"}))
	require.NoError(t, err)
	assert.Equal(t, "Cheer them on.", got.Msg.Personality.Text)
	assert.Equal(t, "own", got.Msg.Personality.Source)

	_, err = cfg.SetConfigValue(ctx, connect.NewRequest(&pb.SetConfigValueRequest{Workspace: dir, Key: "ui.personality", Value: "coach"}))
	require.NoError(t, err)
	all = list()
	assert.True(t, all["coach"].WorkspaceChoice, "the workspace's, at once")
	assert.True(t, all["default"].PersonDefault, "the person's default is unchanged")
	_, err = cfg.SetConfigValue(ctx, connect.NewRequest(&pb.SetConfigValueRequest{Key: "ui.personality", Value: "calm"}))
	require.NoError(t, err)
	assert.True(t, list()["calm"].PersonDefault)
	_, err = cfg.SetConfigValue(ctx, connect.NewRequest(&pb.SetConfigValueRequest{Key: "profile.pronouns", Value: "he/him"}))
	require.NoError(t, err)
	res, err := cfg.ListPersonalities(ctx, connect.NewRequest(&pb.ListPersonalitiesRequest{}))
	require.NoError(t, err)
	assert.Equal(t, "he/him", res.Msg.Profile.Pronouns, "About you")

	tried, err := c.workspaces.TryPersonality(ctx, connect.NewRequest(&pb.TryPersonalityRequest{Workspace: dir, Text: "Be kind."}))
	require.NoError(t, err)
	assert.NotEmpty(t, tried.Msg.Answer, "the suggestions model's answer")
	assert.NotEmpty(t, tried.Msg.Question)

	for name, call := range map[string]func() error{
		"bad name": func() error {
			return unary(cfg.SavePersonality, &pb.SavePersonalityRequest{Personality: &pb.Personality{Name: "A B", Text: "t"}})
		},
		"none":            func() error { return unary(cfg.SavePersonality, &pb.SavePersonalityRequest{}) },
		"unknown":         func() error { return unary(cfg.GetPersonality, &pb.GetPersonalityRequest{Name: "ghost"}) },
		"unknown deleted": func() error { return unary(cfg.DeletePersonality, &pb.DeletePersonalityRequest{Name: "ghost"}) },
		"a bad workspace": func() error { return unary(cfg.ListPersonalities, &pb.ListPersonalitiesRequest{Workspace: "relative"}) },
		"a voice setting": func() error {
			return unary(cfg.SetConfigValue, &pb.SetConfigValueRequest{Workspace: dir, Key: "voice.enabled", Value: "true"})
		},
		"a bad trial workspace": func() error {
			return unary(c.workspaces.TryPersonality, &pb.TryPersonalityRequest{Workspace: "relative", Text: "x"})
		},
		"too long a trial": func() error {
			return unary(c.workspaces.TryPersonality, &pb.TryPersonalityRequest{Workspace: dir, Text: string(make([]byte, 2001))})
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := call()
			require.Error(t, err)
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "%v", err)
		})
	}
	err = unary(cfg.DeletePersonality, &pb.DeletePersonalityRequest{Name: "mentor"})
	assert.Equal(t, "READ_ONLY", reasonOf(err))
	_, err = cfg.DeletePersonality(ctx, connect.NewRequest(&pb.DeletePersonalityRequest{Name: "coach"}))
	require.NoError(t, err)
	assert.NotContains(t, list(), "coach")
}

// Settings that can't be read are said, not hidden.
func TestPersonalityBadSettings(t *testing.T) {
	_, s := serve(t, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	cfg := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	voice := pb.NewVoiceServiceClient(http.DefaultClient, srv.URL)
	dir := filepath.Join(os.Getenv("HOME"), ".blitz")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env.toml"), []byte("[ui\n"), 0o600))
	for name, call := range map[string]func() error{
		"list":     func() error { return unary(cfg.ListPersonalities, &pb.ListPersonalitiesRequest{}) },
		"get":      func() error { return unary(cfg.GetPersonality, &pb.GetPersonalityRequest{Name: "mentor"}) },
		"delete":   func() error { return unary(cfg.DeletePersonality, &pb.DeletePersonalityRequest{Name: "x"}) },
		"describe": func() error { return unary(voice.DescribeVoice, &pb.DescribeVoiceRequest{}) },
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(call()))
		})
	}
}
