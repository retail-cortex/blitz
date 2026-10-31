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

package selfupdate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"
)

// CheckURL is Retail Cortex's update check, which counts installs in use
// (spec_update_checks_054 UC-01).
const CheckURL = "https://license.retailcortex.com/v1/blitz/latest"

// CheckWait is how long the counted check has before GitHub answers
// instead (UC-02).
var CheckWait = 5 * time.Second

// Count is what a counted check sends: nothing else (UC-01).
type Count struct {
	URL     string // CheckURL
	Install string // InstallID's
	Version string // Blitz's
	App     string // "desktop" or "cli"
	GOOS    string
	GOARCH  string
}

// Counting is s with its checks counted (UC-01): as app, at version, with
// the install's ID kept in dir (~/.blitz). on is the settings' [updates]
// count (UC-20). A local release (BLITZ_UPDATE_URL, UC-05), counting off,
// or an ID that can't be read or made leave s as it is.
func (s Source) Counting(on bool, dir, app, version string) Source {
	if !on || os.Getenv("BLITZ_UPDATE_URL") != "" {
		return s
	}
	id, err := InstallID(dir)
	if err != nil {
		return s
	}
	s.Count = &Count{URL: CheckURL, Install: id, Version: version, App: app, GOOS: goruntime.GOOS, GOARCH: goruntime.GOARCH}
	return s
}

// InstallID is the install's ID, 16 random bytes in hex, made the first
// time in dir/install-id and read after (UC-10).
func InstallID(dir string) (string, error) {
	file := filepath.Join(dir, "install-id")
	if data, err := os.ReadFile(file); err == nil {
		if id := strings.TrimSpace(string(data)); validID(id) {
			return id, nil
		}
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(file, []byte(id+"\n"), 0o600); err != nil {
		return "", err
	}
	return id, nil
}

func validID(id string) bool {
	_, err := hex.DecodeString(id)
	return len(id) == 32 && err == nil
}

// latest asks the counted check for the latest release's tag (UC-01).
func (c *Count) latest(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, CheckWait)
	defer cancel()
	q := url.Values{"install": {c.Install}, "version": {c.Version}, "os": {c.GOOS}, "arch": {c.GOARCH}, "app": {c.App}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var r struct {
		Tag string `json:"tag"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil || !strings.HasPrefix(r.Tag, "v") {
		return "", errors.New("no release tag in the answer")
	}
	return r.Tag, nil
}
