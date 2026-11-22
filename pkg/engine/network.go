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

package engine

import (
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/netutil"
)

// SetupNetwork trusts [network] ca_file's certificate authorities besides
// the system's, in every client built via netutil (the model SDKs, MCP,
// web search, web_fetch) without mutating http.DefaultTransport in place
// (spec_027_parity PAR-MOD-05). Proxies come from the environment already.
// The authorities are the process's, as before netutil: a workspace
// without ca_file leaves them be, so opening it in the service doesn't
// take another workspace's away.
func SetupNetwork(cfg *config.Config) error {
	if cfg == nil || cfg.Network.CAFile == "" {
		return nil
	}
	return netutil.SetExtraCAs(config.ExpandHome(cfg.Network.CAFile))
}
