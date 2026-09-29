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

package client

import (
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A notice from the service reaches the front end as api.Event.Notice.
func TestNoticeFromTheService(t *testing.T) {
	got, ok := event(&pb.TurnEvent{Kind: &pb.TurnEvent_Notice{Notice: &pb.Notice{Text: "Couldn't save this message", Error: true}}})
	require.True(t, ok)
	assert.Equal(t, &api.Notice{Text: "Couldn't save this message", Error: true}, got.Notice)
}
