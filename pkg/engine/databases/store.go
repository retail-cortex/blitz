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

package databases

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// ErrUnknown is a connection that isn't there.
var ErrUnknown = errors.New("no such database")

// TestTimeout bounds connecting to a database to test or try it.
var TestTimeout = 20 * time.Second

// secretScope is the secrets scope of a connection's scope.
func secretScope(prefixDir, workspace, scope string) string {
	if scope == ScopeGlobal {
		return secrets.GlobalScope
	}
	return config.SecretScope(prefixDir, workspace)
}

// HasSecret says whether a connection's secret field has a value, in its
// own scope.
func HasSecret(prefixDir, workspace string, c Connection, field string) bool {
	_, ok := config.Vault(prefixDir).Resolve(secretScope(prefixDir, workspace, c.Scope))(secrets.OnlyWorkspace, SecretKey(c.Name, field))
	return ok
}

// Save writes c in its scope's folder, in place of previous (its old name,
// "" for a new one), keeping the secret values given (by field) as Blitz
// secrets in that scope; problems, with nothing written, when it can't be
// saved.
func Save(prefixDir, workspace string, c Connection, values map[string]string, previous string) (Connection, []string, error) {
	if c.Scope == "" {
		c.Scope = ScopeWorkspace
	}
	problems := Check(c)
	if c.Scope != ScopeGlobal && c.Scope != ScopeWorkspace {
		problems = append(problems, fmt.Sprintf("scope %q: workspace or global", c.Scope))
	}
	if c.Scope == ScopeWorkspace && workspace == "" {
		problems = append(problems, "no workspace for a workspace database")
	}
	dir := Dir(prefixDir, workspace, c.Scope)
	if previous != c.Name && path(dir, c.Name) != "" {
		problems = append(problems, fmt.Sprintf("there's a database named %q already", c.Name))
	}
	if previous != "" && path(dir, previous) == "" {
		return Connection{}, nil, fmt.Errorf("%w: %q", ErrUnknown, previous)
	}
	vault := config.Vault(prefixDir)
	scope := secretScope(prefixDir, workspace, c.Scope)
	if kind, ok := KindOf(c.Type); ok && !c.Custom {
		for _, f := range kind.Fields {
			if !f.Secret {
				continue
			}
			given := values[f.Key] != ""
			kept := HasSecret(prefixDir, workspace, c, f.Key) || (previous != "" && previous != c.Name && HasSecret(prefixDir, workspace, Connection{Name: previous, Scope: c.Scope}, f.Key))
			if f.Required && !given && !kept {
				problems = append(problems, f.Key+" is required")
			}
			if given || kept {
				c.Settings = maps.Clone(c.Settings)
				if c.Settings == nil {
					c.Settings = map[string]string{}
				}
				c.Settings[f.Key] = "${" + SecretKey(c.Name, f.Key) + "}"
			}
		}
	}
	if len(problems) > 0 {
		return Connection{}, problems, nil
	}
	text, err := Render(c)
	if err != nil {
		return Connection{}, nil, err
	}
	if kind, ok := KindOf(c.Type); ok && !c.Custom {
		for _, f := range kind.Fields {
			if !f.Secret {
				continue
			}
			key := SecretKey(c.Name, f.Key)
			if v := values[f.Key]; v != "" {
				if err := vault.Set(scope, key, v); err != nil {
					return Connection{}, nil, err
				}
				redact.Remember(v)
			} else if previous != "" && previous != c.Name { // renamed: its secret moves with it
				if v, ok := vault.Resolve(scope)(secrets.OnlyWorkspace, SecretKey(previous, f.Key)); ok {
					if err := vault.Set(scope, key, v); err != nil {
						return Connection{}, nil, err
					}
				}
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Connection{}, nil, err
	}
	file := filepath.Join(dir, c.Name+".yaml")
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		return Connection{}, nil, err
	}
	if previous != "" && previous != c.Name {
		if err := Delete(prefixDir, workspace, c.Scope, previous); err != nil {
			return Connection{}, nil, err
		}
	}
	saved := Parse(c.Name, text)
	saved.Scope, saved.Path = c.Scope, file
	return saved, nil, nil
}

// path is the file of the database name in dir; "" when there's none.
func path(dir, name string) string {
	if !ValidName(name) {
		return ""
	}
	for _, ext := range []string{".yaml", ".yml"} {
		p := filepath.Join(dir, name+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Delete removes the database name from its scope, with its secrets.
func Delete(prefixDir, workspace, scope, name string) error {
	p := path(Dir(prefixDir, workspace, scope), name)
	if p == "" {
		return fmt.Errorf("%w: %q", ErrUnknown, name)
	}
	b, _ := os.ReadFile(p)
	c := Parse(name, string(b))
	if err := os.Remove(p); err != nil {
		return err
	}
	if kind, ok := KindOf(c.Type); ok {
		vault := config.Vault(prefixDir)
		for _, f := range kind.Fields {
			if f.Secret {
				_ = vault.Delete(secretScope(prefixDir, workspace, scope), SecretKey(name, f.Key))
			}
		}
	}
	return nil
}

// ToolInfo is a tool a database offers.
type ToolInfo struct {
	Name        string
	Description string
	// ReadOnly tools run without asking.
	ReadOnly bool
}

// Test connects to c as written (with the secret values given, by field,
// in place of the kept ones) and lists the tools it offers: the error
// says why it can't be reached.
func Test(ctx context.Context, prefixDir, workspace string, c Connection, values map[string]string) ([]ToolInfo, error) {
	var out []ToolInfo
	err := with(ctx, prefixDir, workspace, c, values, func(ctx context.Context, cs *mcp.ClientSession, readOnly []string) error {
		res, err := cs.ListTools(ctx, nil)
		if err != nil {
			return err
		}
		for _, t := range res.Tools {
			out = append(out, ToolInfo{Name: t.Name, Description: t.Description, ReadOnly: slices.Contains(readOnly, t.Name)})
		}
		slices.SortFunc(out, func(a, b ToolInfo) int { return strings.Compare(a.Name, b.Name) })
		return nil
	})
	return out, err
}

// Try runs one of c's tools (named <connection>_<tool>, or tool alone)
// with args and returns what it answered, and whether that's an error.
func Try(ctx context.Context, prefixDir, workspace string, c Connection, values map[string]string, tool string, args map[string]any) (string, bool, error) {
	if !strings.HasPrefix(tool, c.Name+"_") {
		tool = c.Name + "_" + tool
	}
	var text string
	var isErr bool
	err := with(ctx, prefixDir, workspace, c, values, func(ctx context.Context, cs *mcp.ClientSession, _ []string) error {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
		if err != nil {
			return err
		}
		var parts []string
		for _, ct := range res.Content {
			if tc, ok := ct.(*mcp.TextContent); ok {
				parts = append(parts, tc.Text)
			}
		}
		text, isErr = strings.Join(parts, "\n"), res.IsError
		return nil
	})
	return text, isErr, err
}

// with starts c alone, as written, and calls f with a session on it.
func with(ctx context.Context, prefixDir, workspace string, c Connection, values map[string]string, f func(context.Context, *mcp.ClientSession, []string) error) error {
	if problems := Check(c); len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	if kind, ok := KindOf(c.Type); ok && !c.Custom {
		c.Settings = maps.Clone(c.Settings)
		if c.Settings == nil {
			c.Settings = map[string]string{}
		}
		for _, fl := range kind.Fields {
			if fl.Secret && (values[fl.Key] != "" || HasSecret(prefixDir, workspace, c, fl.Key)) {
				c.Settings[fl.Key] = "set" // Render names the secret
			}
		}
	}
	text, err := Render(c)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "blitz-db-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	file := filepath.Join(tmp, c.Name+".yaml")
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		return err
	}
	scope := secretScope(prefixDir, workspace, c.Scope)
	vars := Vars([]string{file}, config.Vault(prefixDir).Resolve(scope))
	for k, v := range values {
		if v != "" {
			vars[SecretKey(c.Name, k)] = v
			redact.Remember(v)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, TestTimeout)
	defer cancel()
	cfg := config.MCPServerConfig{Args: []string{file}, Env: vars}
	client := mcp.NewClient(&mcp.Implementation{Name: "blitz", Version: "studio"}, nil)
	cs, err := client.Connect(ctx, Transport(cfg), nil)
	if err != nil {
		return err
	}
	defer cs.Close()
	var readOnly []string
	if loaded, err := loadConfig(ctx, []string{file}, vars); err == nil {
		readOnly = readOnlyTools(loaded)
	}
	return f(ctx, cs, readOnly)
}
