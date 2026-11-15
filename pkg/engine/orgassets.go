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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
)

// orgVersions is, for each of the organization's agents the person has,
// the folder of each skill version it was published with (spec_053
// EN-82), read from the organization's items.json again when it changes.
type orgVersions struct {
	mu      sync.Mutex
	stamp   time.Time
	byAgent map[string]map[string]string
}

// skillVersions are agent's own versions of its skills, by skill name, to
// their folders: only for the organization's agent of that name, as it's
// loaded (a person's own agent of the name runs the provider's skills).
func (w *Workspace) skillVersions(agent string) map[string]string {
	org := w.settings().Org
	if org.PolicyDir == "" || agent == "" {
		return nil
	}
	spec, ok := w.agents.Get(agent)
	if !ok || !strings.HasPrefix(spec.Path, org.PolicyDir+string(filepath.Separator)) {
		return nil
	}
	st, err := os.Stat(filepath.Join(org.PolicyDir, config.OrgItemsFile))
	if err != nil {
		return nil
	}
	v := &w.orgVersions
	v.mu.Lock()
	defer v.mu.Unlock()
	if !st.ModTime().Equal(v.stamp) || v.byAgent == nil {
		v.byAgent = map[string]map[string]string{}
		for _, a := range org.Assets() {
			if a.Kind != "agent" || !a.On || a.State == "dependency" {
				continue
			}
			m := map[string]string{}
			for _, d := range a.Deps {
				if d.Kind != "skill" || d.Resolved == "" {
					continue
				}
				if dir := org.VersionDir(d.Name, d.Resolved); dirExists(dir) {
					m[d.Name] = dir
				}
			}
			v.byAgent[a.Name] = m
		}
		v.stamp = st.ModTime()
	}
	return v.byAgent[agent]
}

func dirExists(dir string) bool {
	st, err := os.Stat(dir)
	return err == nil && st.IsDir()
}
