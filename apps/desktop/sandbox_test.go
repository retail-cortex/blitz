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

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The settings' sandbox row asks the app for the sandbox's state, which
// is never empty, and it works before Wails gives the app its context.
func TestSandboxStatus(t *testing.T) {
	a := &App{}
	assert.Equal(t, context.Background(), a.context())
	assert.NotEmpty(t, a.SandboxStatus().State)
}
