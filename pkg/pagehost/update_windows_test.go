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
//go:build windows

package pagehost

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/windows"
)

// A file in use is told by its code, whatever language the message is in.
func TestInUse(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"sharing violation": {&os.LinkError{Op: "rename", Err: windows.ERROR_SHARING_VIOLATION}, true},
		"access denied":     {&os.LinkError{Op: "rename", Err: windows.ERROR_ACCESS_DENIED}, false},
		"the English text":  {errors.New("used by another process"), false},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, inUse(tc.err))
		})
	}
}
