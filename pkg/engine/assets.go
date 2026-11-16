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
	"context"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/catalog"
	"github.com/retail-cortex/blitz/pkg/packages"
)

// ValidateAssets checks the workspace's assets (.agents) as Blitz reads
// them (pkg/catalog, the engine's parsers), and what's worth fixing
// (spec_065 PK-14): for `blitz pkg validate`, which runs in the service.
func (w *Workspace) ValidateAssets(context.Context) ([]api.AssetFinding, error) {
	out := []api.AssetFinding{}
	for _, f := range packages.Validate(packages.Root(w.Dir()), CatalogCheck) {
		out = append(out, api.AssetFinding{Key: f.Key, Problems: f.Problems, Warnings: f.Warnings})
	}
	return out, nil
}

// CatalogCheck checks a file of an asset as Blitz reads it: every file but
// a skill's own (its scripts and references, read as it uses them).
func CatalogCheck(p string, data []byte) (bool, []string) {
	k, _, ok := catalog.Of(p)
	if !ok || k == catalog.SkillFile {
		return false, nil
	}
	_, probs := catalog.Check(p, data)
	return true, probs
}
