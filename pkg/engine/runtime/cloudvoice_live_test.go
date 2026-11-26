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
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The paid check of the cloud voice engine (spec_063_voice VOX-76): the
// real Text-to-Speech says a sentence in a tuned Chirp 3: HD voice, and
// the real Speech-to-Text hears it back, with voice activity and its
// words settled. Only with BLITZ_LIVE=1 and Google Cloud credentials
// (gcloud auth application-default login) in a project with both APIs
// on; it costs a few cents:
//
//	bazel test //pkg/engine/runtime:runtime_test --test_filter=TestCloudVoiceLive \
//	  --test_env=BLITZ_LIVE=1 --test_arg=-test.v --test_output=streamed \
//	  --test_env=GOOGLE_APPLICATION_CREDENTIALS=$HOME/.config/gcloud/application_default_credentials.json
//
// (The credentials file is named, as these tests have a home directory
// of their own.) First run 2026-11-26: the first audio 0.4 s after it
// was asked for, and each sentence settled 0.1 s after it ended.
//
// BLITZ_HEAR_MODEL and BLITZ_HEAR_LOCATION try another Speech-to-Text
// model and location ([voice] hear_model, hear_location).
func TestCloudVoiceLive(t *testing.T) {
	if os.Getenv("BLITZ_LIVE") != "1" {
		t.Skip("BLITZ_LIVE=1 and Google Cloud credentials run it")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// The sign-in asks for its token on a connection of the test's own,
	// let go at its end (the leak check counts it).
	client := &http.Client{Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	old := googleCredentials
	defer func() { googleCredentials = old }()
	googleCredentials = func() (*auth.Credentials, error) {
		return credentials.DetectDefault(&credentials.DetectOptions{Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"}, Client: client})
	}
	cfg := config.DefaultConfig()
	cfg.Voice.HearModel, cfg.Voice.HearLocation = os.Getenv("BLITZ_HEAR_MODEL"), os.Getenv("BLITZ_HEAR_LOCATION")
	cv, err := NewCloudVoice(ctx, cfg)
	require.NoError(t, err)
	defer cv.Close()
	t.Logf("project %s", cv.Project)

	// Said: in a locale, slower and lower, as a personality's would be.
	tuning := config.VoiceTuning{Locale: "en-GB", Speed: 0.9, Pitch: -2}
	start := time.Now()
	s, err := cv.Speak(ctx, "Aoede", tuning)
	require.NoError(t, err)
	require.NoError(t, s.Say("Good afternoon. Please run the tests in the cart package."))
	require.NoError(t, s.Finish())
	var pcm []byte
	first := time.Duration(0)
	for {
		piece, err := s.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err, CloudVoiceProblem(err, cv.Project))
		if first == 0 {
			first = time.Since(start)
		}
		pcm = append(pcm, piece...)
	}
	s.Close()
	seconds := float64(len(pcm)) / 2 / speechRate
	t.Logf("said %.1f s of audio, the first of it after %v", seconds, first.Round(time.Millisecond))
	require.Greater(t, seconds, 2.0)

	// Heard: the same audio at Speech-to-Text's 16 kHz, a tenth of a
	// second at a time as a microphone's comes, then quiet.
	down := newPitchShifter(1.5) // 24 kHz read at one and a half times its rate is 16 kHz
	heard16 := append(down.write(pcm), down.flush()...)
	l, err := cv.Listen(ctx, "en-GB")
	require.NoError(t, err)
	defer l.Close()
	// The microphone stops when the test does (the leak check counts it).
	stop, stopped := make(chan struct{}), make(chan struct{})
	defer func() {
		close(stop)
		<-stopped
		l.Close()
		cv.Close()
	}()
	go func() {
		defer close(stopped)
		quiet := make([]byte, 3200)
		send := func(pcm []byte) bool {
			l.Send(pcm)
			select {
			case <-stop:
				return false
			case <-time.After(100 * time.Millisecond):
				return true
			}
		}
		for range 5 {
			if !send(quiet) {
				return
			}
		}
		for i := 0; i < len(heard16); i += 3200 {
			if !send(heard16[i:min(i+3200, len(heard16))]) {
				return
			}
		}
		for range 40 {
			if !send(quiet) {
				return
			}
		}
		l.Flush()
	}()
	var words []string
	kinds := map[string]int{}
	for e := range l.Events() {
		kinds[e.Kind]++
		t.Logf("%6.2fs %s %q", time.Since(start).Seconds(), e.Kind, e.Text)
		require.NotEqual(t, HearError, e.Kind, "%v: %s", e.Err, CloudVoiceProblem(e.Err, cv.Project))
		if e.Kind == HearFinal {
			words = append(words, e.Text)
			if strings.Contains(strings.ToLower(strings.Join(words, " ")), "cart") {
				break
			}
		}
	}
	all := strings.ToLower(strings.Join(words, " "))
	assert.Contains(t, all, "tests")
	assert.Contains(t, all, "cart")
	assert.NotZero(t, kinds[HearBegin], "voice activity is told")
	assert.NotZero(t, kinds[HearInterim], "words come as they're said")
}
