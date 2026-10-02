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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"
	"github.com/BurntSushi/toml"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/i18n"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// configService implements ConfigService over pkg/config: the settings
// files and the keychain. Changes reload the open workspaces they touch.
type configService struct{ s *Server }

// WithConfigDir is the settings directory the service loads from ("" for
// the usual one, ~/.blitz), as the --config flag.
func WithConfigDir(dir string) Option { return func(s *Server) { s.configDir = dir } }

// scope checks a request's scope: "" (global) or an absolute directory,
// returned canonical.
func scope(workspace string) (string, error) {
	if workspace == "" {
		return "", nil
	}
	dir, err := canonical(workspace)
	if err != nil {
		return "", connect.NewError(connect.CodeInvalidArgument, err)
	}
	return dir, nil
}

func invalid(err error) error {
	if err == nil {
		return nil
	}
	return connect.NewError(connect.CodeInvalidArgument, err)
}

var keySources = map[config.KeySource]pb.KeySource{
	config.KeyNone:        pb.KeySource_KEY_SOURCE_NONE,
	config.KeyKeychain:    pb.KeySource_KEY_SOURCE_KEYCHAIN,
	config.KeyPlain:       pb.KeySource_KEY_SOURCE_PLAIN,
	config.KeyObfuscated:  pb.KeySource_KEY_SOURCE_OBFUSCATED,
	config.KeyEnvironment: pb.KeySource_KEY_SOURCE_ENVIRONMENT,
	config.KeyInherited:   pb.KeySource_KEY_SOURCE_INHERITED,
}

func (h configService) DescribeConfig(_ context.Context, r req[pb.DescribeConfigRequest]) (*connect.Response[pb.DescribeConfigResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	info, err := config.Describe(h.s.configDir, dir)
	if err != nil {
		return nil, invalid(err)
	}
	res := &pb.DescribeConfigResponse{Path: info.Path, Provider: info.Provider, DefaultModel: info.DefaultModel, SecretStore: info.SecretStore}
	for _, p := range info.Providers {
		res.Providers = append(res.Providers, &pb.ProviderConfig{
			Name: p.Name, KeySource: keySources[p.KeySource], KeyMissing: p.KeyMissing, BaseUrl: p.BaseURL, Model: p.Model,
			Auth: p.Auth, ProjectId: p.ProjectID, Location: p.Location, Profile: p.Profile,
		})
	}
	return ok(res)
}

// changed reloads the open workspaces a change in scope dir touches (all of
// them for the global settings) and reports the scope's model error.
func (h configService) changed(ctx context.Context, dir, path string) *pb.ConfigChange {
	change := &pb.ConfigChange{Path: path}
	h.s.models.forget() // keys or providers may differ now
	h.s.mu.Lock()
	var ws []*workspace
	for d, w := range h.s.workspaces {
		if dir == "" || d == dir {
			ws = append(ws, w)
		}
	}
	h.s.mu.Unlock()
	for _, w := range ws {
		cfg, err := config.LoadWorkspace(h.s.configDir, w.Dir())
		if err == nil {
			// Rules first: a model error mustn't hide a rule change.
			w.ReloadPermissions(cfg)
			err = w.ReloadProviders(ctx, cfg)
		}
		if dir != "" && err != nil {
			change.ModelError = err.Error()
		}
	}
	return change
}

func (h configService) SetApiKey(ctx context.Context, r req[pb.SetApiKeyRequest]) (*connect.Response[pb.SetApiKeyResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	path, err := config.SetAPIKey(h.s.configDir, dir, r.Msg.Provider, r.Msg.Key)
	if err != nil {
		return nil, invalid(err)
	}
	return ok(&pb.SetApiKeyResponse{Change: h.changed(ctx, dir, path)})
}

func (h configService) RemoveApiKey(ctx context.Context, r req[pb.RemoveApiKeyRequest]) (*connect.Response[pb.RemoveApiKeyResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	path, err := config.RemoveAPIKey(h.s.configDir, dir, r.Msg.Provider)
	if err != nil {
		return nil, invalid(err)
	}
	return ok(&pb.RemoveApiKeyResponse{Change: h.changed(ctx, dir, path)})
}

func (h configService) SecureApiKey(ctx context.Context, r req[pb.SecureApiKeyRequest]) (*connect.Response[pb.SecureApiKeyResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	path, err := config.SecureAPIKey(h.s.configDir, dir, r.Msg.Provider)
	if err != nil {
		return nil, invalid(err)
	}
	return ok(&pb.SecureApiKeyResponse{Change: h.changed(ctx, dir, path)})
}

func (h configService) SetConfigValue(ctx context.Context, r req[pb.SetConfigValueRequest]) (*connect.Response[pb.SetConfigValueResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	path, err := config.SetValue(h.s.configDir, dir, r.Msg.Key, r.Msg.Value)
	if err != nil {
		return nil, invalid(err)
	}
	return ok(&pb.SetConfigValueResponse{Change: h.changed(ctx, dir, path)})
}

func (h configService) SetProvider(ctx context.Context, r req[pb.SetProviderRequest]) (*connect.Response[pb.SetProviderResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	choice := config.ProviderChoice{Provider: r.Msg.Provider, Model: r.Msg.DefaultModel, Key: r.Msg.Key}
	if a := r.Msg.Auth; a != nil {
		choice.Auth = &config.ProviderAuth{Method: a.Method, ProjectID: a.ProjectId, Location: a.Location, Profile: a.Profile}
	}
	path, err := config.SetProvider(h.s.configDir, dir, choice)
	if err != nil {
		return nil, invalid(err)
	}
	return ok(&pb.SetProviderResponse{Change: h.changed(ctx, dir, path)})
}

func entries(p config.PermissionsConfig) []*pb.PermissionEntry {
	var out []*pb.PermissionEntry
	for _, e := range []struct {
		effect string
		rules  []string
	}{{"deny", p.Deny}, {"ask", p.Ask}, {"allow", p.Allow}} {
		for _, r := range e.rules {
			out = append(out, &pb.PermissionEntry{Effect: e.effect, Rule: r})
		}
	}
	return out
}

func (h configService) DescribePermissions(_ context.Context, r req[pb.DescribePermissionsRequest]) (*connect.Response[pb.DescribePermissionsResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	own, path, err := config.ScopePermissions(h.s.configDir, dir)
	if err != nil {
		return nil, invalid(err)
	}
	effective := own
	res := &pb.DescribePermissionsResponse{Path: path, Rules: entries(own), ReadOnlyCommands: config.ReadOnlyCommands, ReadOnlyGuards: config.ReadOnlyGuardLabels}
	if own.ReadOnlyDefaults != nil {
		res.ReadOnlyDefaults = map[bool]string{true: "on", false: "off"}[*own.ReadOnlyDefaults]
	}
	if dir != "" {
		global, _, err := config.ScopePermissions(h.s.configDir, "")
		if err != nil {
			return nil, invalid(err)
		}
		res.Inherited = entries(global)
		effective = global.Merge(own)
	}
	res.ReadOnlyDefaultsOn = effective.DefaultsOn()
	return ok(res)
}

func (h configService) AddPermission(ctx context.Context, r req[pb.AddPermissionRequest]) (*connect.Response[pb.AddPermissionResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	path, rule, err := config.AddPermissionRule(h.s.configDir, dir, r.Msg.Effect, r.Msg.Rule)
	if err != nil {
		return nil, invalid(err)
	}
	return ok(&pb.AddPermissionResponse{Change: h.changed(ctx, dir, path), Rule: rule})
}

func (h configService) RemovePermission(ctx context.Context, r req[pb.RemovePermissionRequest]) (*connect.Response[pb.RemovePermissionResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	path, n, err := config.RemovePermissionRule(h.s.configDir, dir, r.Msg.Rule)
	if err != nil {
		return nil, invalid(err)
	}
	return ok(&pb.RemovePermissionResponse{Change: h.changed(ctx, dir, path), Removed: int32(n)})
}

func (h configService) CheckPermission(_ context.Context, r req[pb.CheckPermissionRequest]) (*connect.Response[pb.CheckPermissionResponse], error) {
	c, err := tools.CheckPermissionRule(tools.Effect(r.Msg.Effect), r.Msg.Rule, r.Msg.Sample)
	if err != nil {
		return ok(&pb.CheckPermissionResponse{Error: err.Error()})
	}
	return ok(&pb.CheckPermissionResponse{Rule: c.Rule, Kind: c.Kind, Pattern: c.Pattern, Form: c.Form, Tested: c.Tested, Matches: c.Matches, Redirect: c.Redirect})
}

func (h configService) SetReadOnlyDefaults(ctx context.Context, r req[pb.SetReadOnlyDefaultsRequest]) (*connect.Response[pb.SetReadOnlyDefaultsResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	var on *bool
	switch r.Msg.Value {
	case "on", "off":
		v := r.Msg.Value == "on"
		on = &v
	case "":
	default:
		return nil, invalid(fmt.Errorf("read_only_defaults %q: on, off or \"\"", r.Msg.Value))
	}
	path, err := config.SetReadOnlyDefaults(h.s.configDir, dir, on)
	if err != nil {
		return nil, invalid(err)
	}
	return ok(&pb.SetReadOnlyDefaultsResponse{Change: h.changed(ctx, dir, path)})
}

func (h configService) GetConfigFile(_ context.Context, r req[pb.GetConfigFileRequest]) (*connect.Response[pb.GetConfigFileResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	path, text, err := config.ReadSettingsFile(h.s.configDir, dir)
	if err != nil {
		return nil, invalid(err)
	}
	return ok(&pb.GetConfigFileResponse{Path: path, Text: text})
}

func (h configService) SaveConfigFile(ctx context.Context, r req[pb.SaveConfigFileRequest]) (*connect.Response[pb.SaveConfigFileResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	path, warnings, err := config.WriteSettingsFile(h.s.configDir, dir, r.Msg.Text)
	if err != nil {
		return nil, invalid(err)
	}
	return ok(&pb.SaveConfigFileResponse{Change: h.changed(ctx, dir, path), Warnings: warnings})
}

func (h configService) CheckConfigFile(_ context.Context, r req[pb.CheckConfigFileRequest]) (*connect.Response[pb.CheckConfigFileResponse], error) {
	var out []*pb.ConfigProblem
	for _, p := range config.CheckSettings(r.Msg.Text) {
		out = append(out, &pb.ConfigProblem{Line: int32(p.Line), Column: int32(p.Col), Error: p.Error, Message: p.Message})
	}
	return ok(&pb.CheckConfigFileResponse{Problems: out})
}

func (h configService) GetSettingsReference(context.Context, req[pb.GetSettingsReferenceRequest]) (*connect.Response[pb.GetSettingsReferenceResponse], error) {
	var out []*pb.SettingInfo
	for _, s := range config.Reference() {
		out = append(out, &pb.SettingInfo{Key: s.Key, Type: s.Type, Default: s.Default, Doc: s.Doc})
	}
	return ok(&pb.GetSettingsReferenceResponse{Settings: out})
}

// userCatalogLimit bounds a user catalog sent to the page, and
// userCatalogs how many are.
const (
	userCatalogLimit = 1 << 20
	userCatalogs     = 20
)

// GetInterfaceLanguage gives the desktop app the terminal's language
// setting and the user's catalogs (BL-DSK-41).
func (h configService) GetInterfaceLanguage(_ context.Context, _ req[pb.GetInterfaceLanguageRequest]) (*connect.Response[pb.GetInterfaceLanguageResponse], error) {
	cfg, err := config.Load(h.s.configDir)
	if err != nil {
		return nil, invalid(err)
	}
	res := &pb.GetInterfaceLanguageResponse{}
	var set struct {
		UI struct {
			Locale *string `toml:"locale"`
		} `toml:"ui"`
	}
	if _, err := toml.DecodeFile(filepath.Join(config.ConfigDir(h.s.configDir), ".env.toml"), &set); err == nil && set.UI.Locale != nil {
		res.Locale = strings.TrimSpace(*set.UI.Locale)
	}
	dir := config.ExpandHome(cfg.UI.LocalesDir)
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		if len(res.Catalogs) == userCatalogs {
			break
		}
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil || len(data) > userCatalogLimit || i18n.CheckCatalog(data) != nil {
			continue // the terminal warns about it
		}
		res.Catalogs = append(res.Catalogs, string(data))
	}
	return ok(res)
}
