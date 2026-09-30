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

package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
)

// Scripts from storage_uri (BL-SK-04): fetched under the web rules (the
// web fetcher's address checks, deny list and per-host approval), checked
// against the SHA-256 their definition pins, and kept by that hash in
// ~/.blitz/skill-cache, so a later run needs no fetch.

// remoteScriptLimit bounds a fetched script.
const remoteScriptLimit = 10 << 20

// SetWeb lets scripts come from storage_uri, fetched under cfg.
func (s *SkillScripts) SetWeb(cfg WebFetchConfig) {
	s.web = newWebFetcher(cfg)
}

// remoteScript is the source of a storage_uri script: from the cache, or
// fetched and checked.
func (s *SkillScripts) remoteScript(ctx context.Context, skill *skills.Skill, sc skills.ScriptDefinition) ([]byte, error) {
	want := strings.ToLower(sc.StorageSHA256)
	cacheDir := s.cacheDir
	if cacheDir == "" {
		cacheDir = config.ExpandHome("~/.blitz/skill-cache")
	}
	cached := filepath.Join(cacheDir, want)
	if b, err := os.ReadFile(cached); err == nil && hexSHA256(b) == want {
		return b, nil
	}
	if s.web == nil {
		return nil, errors.New("the script is fetched from its storage_uri, and web access is off ([web] enabled)")
	}
	if !s.web.cfg.AllowNetwork {
		return nil, errors.New("the script is fetched from its storage_uri, and the sandbox has no network (sandbox.allow_network)")
	}
	raw, err := skills.StorageURL(sc.StorageURI)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if err := s.web.checkURL(u); err != nil {
		return nil, err
	}
	detail := fmt.Sprintf("Fetch script %s of skill %s from %s", sc.Name, skill.Name, raw)
	if err := approveWeb(ctx, s.hooks, s.web.cfg, "run_skill_script", detail, u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.web.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", raw, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: %s", raw, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, remoteScriptLimit+1))
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", raw, err)
	}
	if len(b) > remoteScriptLimit {
		return nil, fmt.Errorf("%s is larger than %d MiB", raw, remoteScriptLimit>>20)
	}
	if got := hexSHA256(b); got != want {
		return nil, fmt.Errorf("%s doesn't match the SHA-256 its definition pins (got %s)", raw, got)
	}
	if err := os.MkdirAll(cacheDir, 0o700); err == nil {
		_ = writePrivate(cached, b)
	}
	return b, nil
}

func hexSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
