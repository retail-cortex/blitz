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

package jobspec

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	cases := map[string]struct {
		in   string
		want Spec
		err  string
	}{
		"a service job": {
			in:   "---\ndescription: The digest\nschedule: Weekdays at 9:30\ntimezone: Europe/Dublin\nruns_on: service\ncatch_up: once\nlimits:\n  timeout: 20m\nagent: writer\n---\nWrite it.\n",
			want: Spec{Description: "The digest", Schedule: "Weekdays at 9:30", Timezone: "Europe/Dublin", RunsOn: Service, CatchUp: "once", Timeout: 20 * time.Minute, Merge: MergeMain},
		},
		"defaults":           {in: "---\nschedule: \"@hourly\"\n---\n", want: Spec{Schedule: "@hourly", RunsOn: Here, CatchUp: "none", Merge: MergeMain}},
		"on request only":    {in: "---\nruns_on: service\n---\nGo.", want: Spec{RunsOn: Service, CatchUp: "none", Merge: MergeMain}},
		"kept on its branch": {in: "---\nruns_on: service\nmerge: never\n---\nGo.", want: Spec{RunsOn: Service, CatchUp: "none", Merge: MergeNever}},
		"a bad merge":        {in: "---\nmerge: sometimes\n---\n", err: `merge "sometimes"`},
		"elsewhere":          {in: "---\nruns_on: the moon\n---\n", err: `runs_on "the moon"`},
		"a bad catch-up":     {in: "---\ncatch_up: all\n---\n", err: `catch_up "all"`},
		"a bad timeout":      {in: "---\nlimits:\n  timeout: soon\n---\n", err: `limits.timeout "soon"`},
		"a bad schedule":     {in: "---\nschedule: whenever\n---\n", err: "whenever"},
		"not YAML":           {in: "---\n: [\n---\n", err: "yaml"},
		"no frontmatter":     {in: "Just text.", err: "missing frontmatter"},
		"an unended one":     {in: "---\nschedule: x\n", err: "ends the frontmatter"},
		"a byte-order mark":  {in: "\xef\xbb\xbf\n---\n---", want: Spec{RunsOn: Here, CatchUp: "none", Merge: MergeMain}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Parse([]byte(c.in))
			if c.err != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), c.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestSplit(t *testing.T) {
	tests := []struct {
		name, in, fm, body string
	}{
		{"front matter and a body", "---\na: 1\n---\nThe body.\n", "a: 1\n", "The body.\n"},
		{"no body", "---\na: 1\n---", "a: 1\n", ""},
		// N26 engine-sub-2#6: only a --- line ends it.
		{"--- inside a value", "---\nprompt_note: a --- b\n----\nb: 2\n---\r\nThe body.", "prompt_note: a --- b\n----\nb: 2\n", "The body."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, body, err := Split([]byte(tt.in))
			require.NoError(t, err)
			assert.Equal(t, tt.fm, string(fm))
			assert.Equal(t, tt.body, string(body))
		})
	}
	_, _, err := Split([]byte("---a: 1\n---\n"))
	assert.ErrorContains(t, err, "start with a line of ---")
	_, _, err = Split([]byte("---\na: 1\n----\n"))
	assert.ErrorContains(t, err, "ends the frontmatter")
}
