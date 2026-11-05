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

package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every string setting named like a credential (…key, …token, …secret,
// …password) is in EachSecret's list: a new one fails here until it's added,
// so config show and the log redactor can't miss it.
func TestEachSecretCoversEveryCredential(t *testing.T) {
	cfg := DefaultConfig()
	var named []string
	n := 0
	var walk func(v reflect.Value, path string)
	walk = func(v reflect.Value, path string) {
		for i := range v.NumField() {
			f, sf := v.Field(i), v.Type().Field(i)
			tag, _, _ := strings.Cut(sf.Tag.Get("toml"), ",")
			if tag == "" || tag == "-" || !sf.IsExported() {
				continue
			}
			switch f.Kind() {
			case reflect.Struct:
				walk(f, path+tag+".")
			case reflect.String:
				low := strings.ToLower(tag)
				for _, word := range []string{"key", "token", "secret", "password"} {
					if strings.HasSuffix(low, word) {
						n++
						f.SetString(fmt.Sprintf("credential-%03d-value", n))
						named = append(named, path+tag)
						break
					}
				}
			}
		}
	}
	walk(reflect.ValueOf(cfg).Elem(), "")
	require.NotEmpty(t, named)
	got := cfg.SecretValues()
	for i, name := range named {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, got, fmt.Sprintf("credential-%03d-value", i+1), "%s isn't in EachSecret", name)
		})
	}
}

// EachSecret covers the maps of secrets too, and writing through a shallow
// copy leaves the original as it was.
func TestEachSecretMapsAndCopies(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LLM.API.Headers = map[string]string{"Authorization": "Bearer gateway-token"}
	cfg.MCP.Servers = []MCPServerConfig{{Name: "s", Env: map[string]string{"T": "mcp-env-token"}, Headers: map[string]string{"A": "mcp-header-token"}}}
	cfg.Hooks.Stop = []HookConfig{{Type: "http", Headers: map[string]string{"X": "hook-header-token"}}}
	all := []string{"Bearer gateway-token", "mcp-env-token", "mcp-header-token", "hook-header-token"}
	assert.Subset(t, cfg.SecretValues(), all)

	cp := *cfg
	cp.EachSecret(func(string) string { return "***" })
	assert.Subset(t, cfg.SecretValues(), all, "the original changed")
	assert.NotContains(t, cp.SecretValues(), "mcp-env-token")
	assert.Equal(t, "***", cp.Hooks.Stop[0].Headers["X"])
}
