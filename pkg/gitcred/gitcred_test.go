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

package gitcred

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRun(t *testing.T) {
	answer := func(_ context.Context, host string) (string, string, bool, error) {
		switch host {
		case "blitz.acme.com":
			return "blitz", "at-1", true, nil
		case "down.acme.com":
			return "", "", false, errors.New("sign in to Blitz")
		}
		return "", "", false, nil
	}
	cases := map[string]struct {
		action, in, out, err string
	}{
		"the service's host": {"get", "protocol=https\nhost=blitz.acme.com\npath=git/plans.git\n\n", "username=blitz\npassword=at-1\n", ""},
		"another host":       {"get", "protocol=https\nhost=github.com\n", "", ""},
		"not over http":      {"get", "protocol=ssh\nhost=blitz.acme.com\n", "", ""},
		"signed out":         {"get", "protocol=https\nhost=down.acme.com\n", "", "sign in to Blitz"},
		"store keeps none":   {"store", "protocol=https\nhost=blitz.acme.com\npassword=x\n", "", ""},
		"erase":              {"erase", "protocol=https\n", "", ""},
		"something else":     {"list", "", "", "isn't get, store or erase"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			err := Run(context.Background(), c.action, strings.NewReader(c.in), &out, answer)
			if c.err != "" {
				assert.ErrorContains(t, err, c.err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, c.out, out.String())
		})
	}
	assert.Equal(t, Request{"a": "1", "b": "x=y"}, Read(strings.NewReader("a=1\nb=x=y\nnope\n\nc=3\n")))
}
