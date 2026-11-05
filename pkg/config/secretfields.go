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
	"maps"
	"reflect"
	"slices"
)

// EachSecret calls f with every credential the settings hold and puts
// back what it returns: the providers' API keys (Azure's included), the
// web search key, the api gateway's headers, http hooks' headers, and MCP
// servers' environment and header values. It's the one list of what's
// secret: `blitz config show` masks with it and the log redactor collects
// with it. Maps and slices are copied before they're written, so a shallow
// copy of a Config can be masked without changing the original.
func (c *Config) EachSecret(f func(string) string) {
	c.EachNamedSecret(func(_, v string) string { return f(v) })
}

// EachNamedSecret is EachSecret, with each value's name: "" for a single
// credential (an API key), else its environment variable's or header's
// name, for callers that treat a map's values by what they're called.
func (c *Config) EachNamedSecret(f func(name, value string) string) {
	for _, p := range c.credentials() {
		*p = f("", *p)
	}
	eachValue := func(m map[string]string) map[string]string {
		if len(m) == 0 {
			return m
		}
		out := maps.Clone(m)
		for k, v := range out {
			out[k] = f(k, v)
		}
		return out
	}
	c.LLM.API.Headers = eachValue(c.LLM.API.Headers)
	c.MCP.Servers = slices.Clone(c.MCP.Servers)
	for i := range c.MCP.Servers {
		c.MCP.Servers[i].Env = eachValue(c.MCP.Servers[i].Env)
		c.MCP.Servers[i].Headers = eachValue(c.MCP.Servers[i].Headers)
	}
	// Every event's hooks: HooksConfig's []HookConfig fields.
	hooks := reflect.ValueOf(&c.Hooks).Elem()
	for i := range hooks.NumField() {
		list, ok := hooks.Field(i).Interface().([]HookConfig)
		if !ok || len(list) == 0 {
			continue
		}
		list = slices.Clone(list)
		for j := range list {
			list[j].Headers = eachValue(list[j].Headers)
		}
		hooks.Field(i).Set(reflect.ValueOf(list))
	}
}

// SecretValues lists the credentials EachSecret visits, empty ones left out.
func (c *Config) SecretValues() []string {
	cp := *c
	var out []string
	cp.EachSecret(func(s string) string {
		if s != "" {
			out = append(out, s)
		}
		return s
	})
	return out
}

// credentials are the settings' single-valued secrets.
func (c *Config) credentials() []*string {
	return []*string{&c.LLM.Gemini.APIKey, &c.LLM.Anthropic.APIKey, &c.LLM.OpenAI.APIKey, &c.LLM.Azure.APIKey, &c.Web.SearchAPIKey}
}
