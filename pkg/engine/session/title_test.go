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

package session

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTitleFrom(t *testing.T) {
	cases := map[string]string{
		"\n\n  fix   the\tlogin bug  \nmore detail": "fix the login bug",
		"   ":                   "",
		strings.Repeat("é", 80): strings.Repeat("é", 59) + "…",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			got := TitleFrom(in)
			assert.Equal(t, want, got, "TitleFrom(%q) = %q, want %q", in, got, want)
		})
	}
}

func TestSessionsAreNamedByTheirFirstPromptAndRenamed(t *testing.T) {
	st, _ := newStorage(t)
	rec, _ := st.CreateSession("", "", "a")
	require.Equal(t, "", rec.Title, "title %q before any prompt", rec.Title)
	st.AddMessage("model", "hello from the model")
	st.AddMessage("user", "add retries to the client")
	st.AddMessage("user", "and tests")
	got := st.Active().Title
	require.Equal(t, "add retries to the client", got, "title %q", got)
	require.NoError(t, st.Rename("  Retry work  "))
	require.Error(t, st.Rename(" \n "), "empty name accepted")
	loaded, err := st.Load(rec.ID)
	require.NoError(t, err, "after reload: %q", loaded.Title)
	require.Equal(t, "Retry work", loaded.Title, "after reload: %q %v", loaded.Title, err)
	// An explicit title is kept.
	st.CreateSession("", "given", "a")
	st.AddMessage("user", "first prompt")
	require.Equal(t, "given", st.Active().Title, "explicit title replaced: %q", st.Active().Title)
}
