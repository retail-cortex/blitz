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

package urn

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	ws  = "9b2f0c4e-1d2a-4b7e-9f00-1c2d3e4f5a6b"
	job = "51aa7e10-0b6c-4c2a-8d1e-2f3a4b5c6d7e"
	run = "e8030f9a-7b1c-4d2e-a3f4-5b6c7d8e9f01"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name, in, out string
		want          Name
	}{
		{name: "a run", in: "urn:blitz:runs:" + run, want: Name{Kind: Runs, IDs: []string{run}}},
		{name: "any case", in: "URN:Blitz:runs:" + "E8030F9A-7B1C-4D2E-A3F4-5B6C7D8E9F01", out: "urn:blitz:runs:" + run, want: Name{Kind: Runs, IDs: []string{run}}},
		{name: "a collection", in: "urn:blitz:jobs", want: Name{Kind: Jobs}},
		{name: "a run's artifacts", in: "urn:blitz:artifacts:" + ws + ":" + job + ":" + run, want: Name{Kind: Artifacts, IDs: []string{ws, job, run}}},
		{name: "one job's artifacts", in: "urn:blitz:artifacts:" + ws + ":" + job, want: Name{Kind: Artifacts, IDs: []string{ws, job}}},
		{name: "a file", in: "urn:blitz:artifacts:" + ws + ":" + job + ":" + run + "/out/sales%20by%20week.png",
			want: Name{Kind: Artifacts, IDs: []string{ws, job, run}, Path: "out/sales by week.png"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := Parse(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, n)
			out := tc.out
			if out == "" {
				out = tc.in
			}
			assert.Equal(t, out, n.String())
		})
	}
}

func TestParseRefuses(t *testing.T) {
	for _, in := range []string{
		"",
		"urn:other:runs:" + run,
		"urn:blitz:",
		"urn:blitz:moons:" + run,
		"urn:blitz:runs:not-an-id",
		"urn:blitz:runs:" + run + ":" + run,
		"urn:blitz:runs:" + run + "/a.txt",
		"urn:blitz:artifacts:" + ws + ":" + job + "/a.txt",
		"urn:blitz:artifacts:" + ws + ":" + job + ":" + run + "/",
		"urn:blitz:artifacts:" + ws + ":" + job + ":" + run + "/a//b",
		"urn:blitz:artifacts:" + ws + ":" + job + ":" + run + "/../x",
		"urn:blitz:artifacts:" + ws + ":" + job + ":" + run + "/a%2Fb",
		"urn:blitz:artifacts:" + ws + ":" + job + ":" + run + "/a%zz",
	} {
		t.Run(in, func(t *testing.T) {
			_, err := Parse(in)
			assert.ErrorIs(t, err, ErrName)
		})
	}
}

func TestNew(t *testing.T) {
	n, err := New(Artifacts, ws, job, run)
	require.NoError(t, err)
	assert.False(t, n.IsCollection())
	f, err := n.File("out/report.csv")
	require.NoError(t, err)
	assert.Equal(t, "urn:blitz:artifacts:"+ws+":"+job+":"+run+"/out/report.csv", f.String())

	p, ok := f.Parent()
	require.True(t, ok)
	assert.Equal(t, n, p)
	p, ok = n.Parent()
	require.True(t, ok)
	assert.Equal(t, "urn:blitz:artifacts:"+ws+":"+job, p.String())
	_, ok = Collection(Runs).Parent()
	assert.False(t, ok)
	assert.True(t, Collection(Runs).IsCollection())

	_, err = New(Runs, "x")
	require.ErrorIs(t, err, ErrName)
	r, err := New(Runs, run)
	require.NoError(t, err)
	_, err = r.File("a.txt")
	require.ErrorIs(t, err, ErrName)
}

func TestText(t *testing.T) {
	n, err := New(Jobs, job)
	require.NoError(t, err)
	b, err := json.Marshal(struct{ URN Name }{n})
	require.NoError(t, err)
	assert.JSONEq(t, `{"URN": "urn:blitz:jobs:`+job+`"}`, string(b))

	var back struct{ URN Name }
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, n, back.URN)
	assert.Error(t, json.Unmarshal([]byte(`{"URN": "urn:blitz:moons"}`), &back))
}
