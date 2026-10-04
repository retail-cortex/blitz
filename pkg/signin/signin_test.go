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

package signin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	anthropicconfig "github.com/anthropics/anthropic-sdk-go/config"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTools puts scripts named for the vendor tools in a folder, and has
// find look only there; body is each one's shell.
func fakeTools(t *testing.T, bodies map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for tool, body := range bodies {
		require.NoError(t, os.WriteFile(filepath.Join(dir, tool), []byte("#!/bin/sh\n"+body+"\n"), 0o755))
	}
	old := find
	find = func(name string) string {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err != nil {
			return ""
		}
		return p
	}
	t.Cleanup(func() { find = old })
}

// collect gathers a sign-in's events.
func collect() (*[]Event, func(Event)) {
	var mu sync.Mutex
	var got []Event
	return &got, func(e Event) { mu.Lock(); got = append(got, e); mu.Unlock() }
}

// A URL and a device code are pulled out of a line.
func TestParse(t *testing.T) {
	tests := []struct {
		line, url, code string
	}{
		{"Go to https://accounts.google.com/o/oauth2/auth?x=1&y=2.", "https://accounts.google.com/o/oauth2/auth?x=1&y=2", ""},
		{"Then enter the code: ABCD-12EF", "", "ABCD-12EF"},
		{"Open (https://device.sso.us-east-1.amazonaws.com/) and enter WXYZ-9876", "https://device.sso.us-east-1.amazonaws.com/", "WXYZ-9876"},
		{"Waiting...", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			e := parse(tt.line)
			assert.Equal(t, tt.line, e.Line)
			assert.Equal(t, tt.url, e.URL)
			assert.Equal(t, tt.code, e.Code)
		})
	}
}

// Each provider runs its own tool's sign-in and sign-out, with the
// profile where the tool takes one.
func TestSignInAndOut(t *testing.T) {
	fakeTools(t, map[string]string{
		"gcloud": `echo "gcloud $*"; echo "Go to https://accounts.google.com/auth"`,
		"ant":    `echo "ant $* profile=$ANTHROPIC_PROFILE"`,
		"aws":    `echo "aws $*"; echo "enter ABCD-1234 at https://device.sso.aws/"`,
		"az":     `echo "az $*"`,
	})
	var r Runner
	ctx := context.Background()
	tests := []struct {
		provider, profile, in, out string
	}{
		{Google, "", "gcloud auth application-default login", "gcloud auth application-default revoke --quiet"},
		{Anthropic, "work", "ant auth login profile=work", "ant auth logout profile=work"},
		{AWS, "dev", "aws sso login --profile dev", "aws sso logout --profile dev"},
		{AWS, "", "aws sso login", "aws sso logout"},
		{Azure, "", "az login", "az logout"},
	}
	for _, tt := range tests {
		t.Run(tt.provider+"/"+tt.profile, func(t *testing.T) {
			got, on := collect()
			require.NoError(t, r.SignIn(ctx, tt.provider, tt.profile, on))
			require.NotEmpty(t, *got)
			assert.Equal(t, tt.in, (*got)[0].Line)
			require.NoError(t, r.SignOut(ctx, tt.provider, tt.profile))
		})
	}
	got, on := collect()
	require.NoError(t, r.SignIn(ctx, AWS, "", on))
	assert.Equal(t, "https://device.sso.aws/", (*got)[1].URL)
	assert.Equal(t, "ABCD-1234", (*got)[1].Code)
}

// A failed sign-in says why, with the tool's last lines; a missing tool
// says where to get it; a bad provider or profile is refused.
func TestSignInFails(t *testing.T) {
	fakeTools(t, map[string]string{"gcloud": `echo "one"; echo "two"; echo "denied" >&2; exit 2`})
	var r Runner
	ctx := context.Background()
	err := r.SignIn(ctx, Google, "", func(Event) {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "one / two / denied")

	err = r.SignIn(ctx, Azure, "", func(Event) {})
	assert.ErrorIs(t, err, ErrNoTool)
	assert.Contains(t, err.Error(), "learn.microsoft.com")
	assert.ErrorIs(t, r.SignIn(ctx, "openai", "", func(Event) {}), ErrUnknownProvider)
	assert.ErrorIs(t, r.SignIn(ctx, AWS, "dev; rm -rf /", func(Event) {}), ErrBadProfile)
	assert.ErrorIs(t, r.SignOut(ctx, "nope", ""), ErrUnknownProvider)
}

// One sign-in per provider at a time; a cancelled one stops at once.
func TestSignInOneAtATimeAndCancel(t *testing.T) {
	fakeTools(t, map[string]string{"az": `echo "waiting"; exec sleep 5`})
	var r Runner
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		var once sync.Once
		done <- r.SignIn(ctx, Azure, "", func(Event) { once.Do(func() { close(started) }) })
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("the sign-in didn't start")
	}
	assert.ErrorIs(t, r.SignIn(context.Background(), Azure, "", func(Event) {}), ErrRunning)
	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("the sign-in didn't stop")
	}
	fakeTools(t, map[string]string{"az": `echo "again"`})
	assert.NoError(t, r.SignIn(context.Background(), Azure, "", func(Event) {}), "free again")
}

// A sign-in that outlasts its limit stops.
func TestSignInTimesOut(t *testing.T) {
	fakeTools(t, map[string]string{"gcloud": `exec sleep 5`})
	old := timeout
	timeout = 200 * time.Millisecond
	t.Cleanup(func() { timeout = old })
	var r Runner
	err := r.SignIn(context.Background(), Google, "", func(Event) {})
	assert.True(t, errors.Is(err, context.DeadlineExceeded), "%v", err)
}

// Each provider's status, from what its sign-in leaves behind.
func TestStatus(t *testing.T) {
	fakeTools(t, map[string]string{"gcloud": "true"})
	old := statusTimeout
	statusTimeout = 2 * time.Second
	t.Cleanup(func() { statusTimeout = old })
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	cfg := config.DefaultConfig()

	t.Run("google", func(t *testing.T) {
		adc := filepath.Join(t.TempDir(), "adc.json")
		t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", adc)
		s, err := StatusOf(ctx, cfg, Google, "")
		require.NoError(t, err)
		assert.True(t, s.ToolFound)
		assert.Equal(t, "gcloud", s.Tool)
		assert.False(t, s.SignedIn, "no file")
		require.NoError(t, os.WriteFile(adc, []byte("{}"), 0o600))
		s, _ = StatusOf(ctx, cfg, Google, "")
		assert.False(t, s.SignedIn, "not credentials")
		require.NoError(t, os.WriteFile(adc, []byte(`{"type":"authorized_user","quota_project_id":"shop-prod"}`), 0o600))
		s, _ = StatusOf(ctx, cfg, Google, "")
		assert.True(t, s.SignedIn)
		assert.Contains(t, s.Detail, "shop-prod")
		t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
		t.Setenv("CLOUDSDK_CONFIG", t.TempDir())
		s, _ = StatusOf(ctx, cfg, Google, "")
		assert.False(t, s.SignedIn, "gcloud's file isn't there")
	})

	t.Run("anthropic", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("ANTHROPIC_CONFIG_DIR", dir)
		t.Setenv("ANTHROPIC_PROFILE", "")
		s, err := StatusOf(ctx, cfg, Anthropic, "work")
		require.NoError(t, err)
		assert.False(t, s.ToolFound)
		assert.False(t, s.SignedIn, "no profile")
		require.NoError(t, anthropicconfig.SaveProfile(dir, "work", &anthropicconfig.Config{AuthenticationInfo: &anthropicconfig.AuthenticationInfo{
			Type: anthropicconfig.AuthenticationTypeUserOAuth, UserOAuth: &anthropicconfig.UserOAuth{ClientID: "c", Scope: "user:inference"},
		}}))
		s, _ = StatusOf(ctx, cfg, Anthropic, "work")
		assert.False(t, s.SignedIn, "a profile with no credentials yet")
		creds := anthropicconfig.ProfileCredentialsPath(dir, "work")
		require.NoError(t, os.MkdirAll(filepath.Dir(creds), 0o700))
		require.NoError(t, os.WriteFile(creds, []byte(`{"access_token":"x"}`), 0o600))
		s, _ = StatusOf(ctx, cfg, Anthropic, "work")
		assert.True(t, s.SignedIn)
		assert.Equal(t, "profile work (user:inference)", s.Detail)
	})

	t.Run("aws", func(t *testing.T) {
		t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
		t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "credentials"))
		t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
		t.Setenv("AWS_PROFILE", "")
		t.Setenv("AWS_ACCESS_KEY_ID", "")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "")
		start := time.Now()
		s, _ := StatusOf(ctx, cfg, AWS, "")
		assert.False(t, s.SignedIn)
		assert.Contains(t, s.Detail, "no AWS profile")
		assert.Less(t, time.Since(start), 500*time.Millisecond, "nothing set up: no probing")
		t.Setenv("AWS_ACCESS_KEY_ID", "AKIDEXAMPLE")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
		s, _ = StatusOf(ctx, cfg, AWS, "")
		assert.True(t, s.SignedIn)
		assert.Contains(t, s.Detail, "Env")
		s, _ = StatusOf(ctx, cfg, AWS, "missing")
		assert.False(t, s.SignedIn, "a profile that isn't configured")
	})

	t.Run("azure", func(t *testing.T) {
		for _, k := range []string{"AZURE_CLIENT_ID", "AZURE_TENANT_ID", "AZURE_CLIENT_SECRET", "AZURE_FEDERATED_TOKEN_FILE", "IDENTITY_ENDPOINT", "MSI_ENDPOINT"} {
			t.Setenv(k, "")
		}
		t.Setenv("PATH", t.TempDir()) // no az, no azd
		start := time.Now()
		s, _ := StatusOf(ctx, cfg, Azure, "")
		assert.False(t, s.SignedIn)
		assert.NotEmpty(t, s.Detail)
		assert.Less(t, time.Since(start), 500*time.Millisecond, "nothing set up: no probing")
		assert.Contains(t, s.Detail, "no Azure CLI")
	})

	_, err := StatusOf(ctx, cfg, "nope", "")
	assert.ErrorIs(t, err, ErrUnknownProvider)
	assert.Equal(t, "a", firstLine("a\nb"))
	assert.True(t, strings.HasPrefix(Providers[0], "g"))
}

// Blank lines are skipped, a failure keeps the last five lines, an
// Anthropic sign-in without a profile uses ant's own, and a tool that
// can't start says so.
func TestSignInLines(t *testing.T) {
	fakeTools(t, map[string]string{
		"gcloud": `for i in 1 2 3 4 5 6 7; do echo "line $i"; echo; done; exit 1`,
		"ant":    `echo "profile=[$ANTHROPIC_PROFILE]"`,
	})
	var r Runner
	got, on := collect()
	err := r.SignIn(context.Background(), Google, "", on)
	require.Error(t, err)
	assert.Len(t, *got, 7, "no blank lines")
	assert.Contains(t, err.Error(), "line 3 / line 4 / line 5 / line 6 / line 7")
	assert.NotContains(t, err.Error(), "line 2")

	t.Setenv("ANTHROPIC_PROFILE", "")
	got, on = collect()
	require.NoError(t, r.SignIn(context.Background(), Anthropic, "", on))
	assert.Equal(t, "profile=[]", (*got)[0].Line)

	old := find
	notExec := filepath.Join(t.TempDir(), "az")
	require.NoError(t, os.WriteFile(notExec, []byte("not a program"), 0o644))
	find = func(string) string { return notExec }
	t.Cleanup(func() { find = old })
	assert.Error(t, r.SignIn(context.Background(), Azure, "", func(Event) {}))
}

// The details: a service account's email, an AWS profile and region, and
// Azure's error when its CLI can't give a token.
func TestStatusDetails(t *testing.T) {
	old := statusTimeout
	statusTimeout = 2 * time.Second
	t.Cleanup(func() { statusTimeout = old })
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	adc := filepath.Join(t.TempDir(), "sa.json")
	require.NoError(t, os.WriteFile(adc, []byte(`{"type":"service_account","client_email":"blitz@shop.iam.gserviceaccount.com"}`), 0o600))
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", adc)
	s, err := StatusOf(ctx, config.DefaultConfig(), Google, "")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(s.Detail, "blitz@shop.iam.gserviceaccount.com ("), s.Detail)

	creds := filepath.Join(t.TempDir(), "credentials")
	require.NoError(t, os.WriteFile(creds, []byte("[dev]\naws_access_key_id = AKIDEXAMPLE\naws_secret_access_key = secret\n"), 0o600))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", creds)
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	for _, k := range []string{"AWS_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"} {
		t.Setenv(k, "")
	}
	cfg := config.DefaultConfig()
	cfg.LLM.Bedrock.Region = "us-east-1"
	cfg.LLM.Bedrock.Profile = "dev"
	s, _ = StatusOf(ctx, cfg, AWS, "")
	assert.True(t, s.SignedIn, s.Detail)
	assert.True(t, strings.HasPrefix(s.Detail, "profile dev, "), s.Detail)

	for _, k := range []string{"AZURE_CLIENT_ID", "AZURE_TENANT_ID", "AZURE_CLIENT_SECRET", "AZURE_FEDERATED_TOKEN_FILE", "IDENTITY_ENDPOINT", "MSI_ENDPOINT"} {
		t.Setenv(k, "")
	}
	fakeTools(t, map[string]string{"az": "echo 'Please run az login' >&2; exit 1"})
	t.Setenv("PATH", filepath.Dir(find("az"))+":/usr/bin:/bin")
	start := time.Now()
	s, _ = StatusOf(ctx, cfg, Azure, "")
	assert.False(t, s.SignedIn)
	assert.NotEmpty(t, s.Detail)
	assert.Less(t, time.Since(start), 3*time.Second, "bounded by statusTimeout")
}
