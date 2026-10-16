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

package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShownVersion(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"0.6.2", "v0.6.2"},
		{"0.7.0-rc.1", "v0.7.0-rc.1"},
		{"0.6.2-3-gabc1234", "v0.6.2-3-gabc1234"},
		{"dev", "dev"},
		{"abc1234-dirty.5f3e2a1", "abc1234-dirty.5f3e2a1"},
		{"", ""},
	} {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, ShownVersion(tc.in))
		})
	}
}
