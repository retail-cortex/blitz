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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelsCommand(t *testing.T) {
	isolate(t)
	_, err := runCLI(t, "models")
	assert.Equal(t, exitUsage, exitCodeFor(err), "no provider set up: %v", err)
	out, err := runCLI(t, "models", "carrier-pigeon")
	assert.Equal(t, exitFailure, exitCodeFor(err))
	assert.Contains(t, out, "can't list its models: unknown provider carrier-pigeon")
}
