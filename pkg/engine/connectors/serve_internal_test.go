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

package connectors

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A saved query's parameter as JSON or a template gave it, made its
// declared type (spec_059_data DAT-20).
func TestParamValue(t *testing.T) {
	for _, tc := range []struct {
		v    any
		typ  string
		want any
	}{
		{4.0, "integer", int64(4)},
		{4.5, "integer", 4.5},
		{" 7 ", "integer", int64(7)},
		{"seven", "integer", "seven"},
		{"2.5", "float", 2.5},
		{"x", "float", "x"},
		{"true", "boolean", true},
		{"maybe", "boolean", "maybe"},
		{"Ada", "string", "Ada"},
		{json.Number("3"), "integer", int64(3)},
		{json.Number("3"), "float", 3.0},
		{json.Number("2.5"), "integer", 2.5},
		{true, "boolean", true},
	} {
		assert.Equal(t, tc.want, paramValue(tc.v, tc.typ), "%v as %s", tc.v, tc.typ)
	}
}
