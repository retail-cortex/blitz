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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/retail-cortex/blitz/pkg/config"
)

var networkOnce sync.Mutex

// SetupNetwork trusts [network] ca_file's certificate authorities besides
// the system's, in every client built from Go's default transport (the
// model SDKs, MCP, web search) and in web_fetch (spec_parity_027
// PAR-MOD-05). Proxies come from the environment already.
func SetupNetwork(cfg *config.Config) error {
	file := cfg.Network.CAFile
	if file == "" {
		return nil
	}
	pem, err := os.ReadFile(config.ExpandHome(file))
	if err != nil {
		return fmt.Errorf("[network] ca_file: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return fmt.Errorf("[network] ca_file %s: no PEM certificates in it", file)
	}
	networkOnce.Lock()
	defer networkOnce.Unlock()
	t := http.DefaultTransport.(*http.Transport)
	tc := &tls.Config{MinVersion: tls.VersionTLS12}
	if t.TLSClientConfig != nil {
		tc = t.TLSClientConfig.Clone()
	}
	tc.RootCAs = pool
	t.TLSClientConfig = tc
	return nil
}
