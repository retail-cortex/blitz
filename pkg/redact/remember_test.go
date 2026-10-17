// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package redact

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Values remembered at run time are masked by every redactor, a nil one
// included, and by Mask; short ones are ignored.
func TestRemember(t *testing.T) {
	Remember("remembered-secret-1", "short")
	assert.Equal(t, "x "+mask+" y", New().String("x remembered-secret-1 y"))
	var none *Redactor
	assert.Equal(t, mask, none.String("remembered-secret-1"))
	assert.Equal(t, "a "+mask, Mask("a remembered-secret-1"))
	assert.Equal(t, "short", Mask("short"))
	Remember("remembered-secret-1") // again: once
	assert.Equal(t, mask+mask, Mask("remembered-secret-1remembered-secret-1"))
}
