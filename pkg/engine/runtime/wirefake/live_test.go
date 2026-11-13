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

package wirefake

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recorder is a test the fake reports to, keeping what it said was wrong.
type recorder struct {
	testing.TB
	mu   sync.Mutex
	errs []string
}

func (r *recorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

func (r *recorder) said() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.errs, "\n")
}

var goodSetup = map[string]any{"setup": map[string]any{
	"model": "models/gemini-live",
	"generationConfig": map[string]any{
		"responseModalities": []any{"AUDIO"},
		"speechConfig":       map[string]any{"voiceConfig": map[string]any{"prebuiltVoiceConfig": map[string]any{"voiceName": "Kore"}}},
	},
}}

func dial(t *testing.T, f *Live, path, key string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	h := http.Header{}
	h.Set("x-goog-api-key", key)
	return websocket.DefaultDialer.Dial(f.URL+path, h)
}

func readOne(c *websocket.Conn) (map[string]any, error) {
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	var m map[string]any
	err := c.ReadJSON(&m)
	return m, err
}

// The fake refuses what the Live API refuses (spec_063_voice VOX-60): the
// wrong socket or key, a first message that isn't a good setup, and,
// once set up, anything but realtime input, client content and responses
// to calls it made.
func TestLiveRefuses(t *testing.T) {
	audio := func(mime string) map[string]any {
		return map[string]any{"realtimeInput": map[string]any{"audio": map[string]any{"mimeType": mime, "data": base64.StdEncoding.EncodeToString([]byte{1, 2})}}}
	}
	setupWith := func(change func(s map[string]any)) map[string]any {
		s := map[string]any{"model": "models/gemini-live", "generationConfig": map[string]any{
			"responseModalities": []any{"AUDIO"},
			"speechConfig":       map[string]any{"voiceConfig": map[string]any{"prebuiltVoiceConfig": map[string]any{"voiceName": "Kore"}}},
		}}
		change(s)
		return map[string]any{"setup": s}
	}
	for name, tc := range map[string]struct {
		setup map[string]any
		then  map[string]any
		want  string
	}{
		"not setup first":    {map[string]any{"clientContent": map[string]any{}}, nil, "the first message must be setup"},
		"a bad model":        {setupWith(func(s map[string]any) { s["model"] = "gemini-live" }), nil, "isn't models/<name>"},
		"text answers":       {setupWith(func(s map[string]any) { s["generationConfig"].(map[string]any)["responseModalities"] = []any{"TEXT"} }), nil, "AUDIO alone"},
		"no voice":           {setupWith(func(s map[string]any) { delete(s["generationConfig"].(map[string]any), "speechConfig") }), nil, "no prebuilt voice"},
		"setup twice":        {goodSetup, goodSetup, "setup again"},
		"two kinds":          {goodSetup, map[string]any{"clientContent": map[string]any{}, "realtimeInput": map[string]any{}}, "one kind of message"},
		"unknown":            {goodSetup, map[string]any{"hello": map[string]any{}}, "unknown message"},
		"audio at 48 kHz":    {goodSetup, audio("audio/pcm;rate=48000"), "not audio/pcm;rate=16000"},
		"audio not base64":   {goodSetup, map[string]any{"realtimeInput": map[string]any{"audio": map[string]any{"mimeType": "audio/pcm;rate=16000", "data": "!!"}}}, "isn't base64"},
		"realtime other":     {goodSetup, map[string]any{"realtimeInput": map[string]any{"video": map[string]any{}}}, `realtimeInput has "video"`},
		"a response unasked": {goodSetup, map[string]any{"toolResponse": map[string]any{"functionResponses": []any{map[string]any{"id": "x"}}}}, "which no call asked for"},
	} {
		t.Run(name, func(t *testing.T) {
			r := &recorder{TB: t}
			f := NewLive(r, "k")
			c, _, err := dial(t, f, LivePath, "k")
			require.NoError(t, err)
			defer c.Close()
			require.NoError(t, c.WriteJSON(tc.setup))
			if tc.then != nil {
				m, err := readOne(c)
				require.NoError(t, err)
				require.Contains(t, m, "setupComplete")
				require.NoError(t, c.WriteJSON(tc.then))
			}
			_, err = readOne(c)
			assert.Error(t, err, "closed")
			assert.Contains(t, r.said(), tc.want)
		})
	}

	t.Run("wrong socket and key", func(t *testing.T) {
		r := &recorder{TB: t}
		f := NewLive(r, "k")
		_, res, err := dial(t, f, "/ws/other", "k")
		require.Error(t, err)
		assert.Equal(t, http.StatusNotFound, res.StatusCode)
		_, res, err = dial(t, f, LivePath, "nope")
		require.Error(t, err)
		assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
		assert.Contains(t, r.said(), "dialled /ws/other")
		assert.Contains(t, r.said(), `x-goog-api-key "nope"`)
	})

	t.Run("not JSON", func(t *testing.T) {
		r := &recorder{TB: t}
		f := NewLive(r, "k")
		c, _, err := dial(t, f, LivePath, "k")
		require.NoError(t, err)
		defer c.Close()
		require.NoError(t, c.WriteMessage(websocket.TextMessage, []byte("{")))
		_, err = readOne(c)
		assert.Error(t, err)
		assert.Contains(t, r.said(), "not JSON")
	})

	t.Run("resumed with another handle", func(t *testing.T) {
		r := &recorder{TB: t}
		f := NewLive(r, "k")
		c, _, err := dial(t, f, LivePath, "k")
		require.NoError(t, err)
		require.NoError(t, c.WriteJSON(goodSetup))
		_, err = readOne(c)
		require.NoError(t, err)
		f.Opened(time.Second)
		f.Resume("h1")
		f.Drop()
		c.Close()
		c2, _, err := dial(t, f, LivePath, "k")
		require.NoError(t, err)
		defer c2.Close()
		require.NoError(t, c2.WriteJSON(goodSetup)) // no handle
		_, err = readOne(c2)
		for err == nil {
			_, err = readOne(c2) // the resumption update, then the close
		}
		assert.Contains(t, r.said(), `resumed with handle "", not the last given, "h1"`)
		assert.Len(t, f.Setups(), 1)
	})
}

// What the client sends is there to wait for; nothing in time, or no
// connection to send on, fails the test.
func TestLiveWaits(t *testing.T) {
	f := NewLive(t, "k")
	c, _, err := dial(t, f, LivePath, "k")
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.WriteJSON(goodSetup))
	_, err = readOne(c)
	require.NoError(t, err)
	f.Opened(time.Second)
	require.NoError(t, c.WriteJSON(map[string]any{"clientContent": map[string]any{"turns": []any{}}}))
	assert.Contains(t, f.Next("clientContent", time.Second), "turns")
	f.Heard("hi")
	m, err := readOne(c)
	require.NoError(t, err)
	assert.Equal(t, "hi", m["serverContent"].(map[string]any)["inputTranscription"].(map[string]any)["text"])
}
