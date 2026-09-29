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

package server

import (
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A notice goes to clients as its own kind of turn event.
func TestNoticeEvent(t *testing.T) {
	for _, n := range []api.Notice{{Text: "Couldn't save this message", Error: true}, {Text: "Using the fallback model"}} {
		got := eventMsg(api.Event{Notice: &n})
		k, ok := got.Kind.(*pb.TurnEvent_Notice)
		require.True(t, ok, "kind %T", got.Kind)
		assert.Equal(t, n.Text, k.Notice.Text)
		assert.Equal(t, n.Error, k.Notice.Error)
	}
}
