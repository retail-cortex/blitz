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

package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/engine/frontmatter"
	"gopkg.in/yaml.v3"
)

// A skill's scenarios (spec_skills_013 SK-111) are Castor's behaviour
// tests (FR-8, ScenarioDefinition): scenarios/<name>.md, its front matter
// the prompt, whether the skill should be used for it (executes), which
// skills must be (expected_skills, or skills_applied), and the threshold
// the answer must reach against the body, the outcome expected. A skill
// published to Castor carries them; cstr test runs them as Blitz does.

// DefaultScenarioThreshold is a scenario's threshold when it gives none.
const DefaultScenarioThreshold = 0.8

// Scenario is one of a skill's scenarios.
type Scenario struct {
	// Name is its file's, without .md.
	Name        string
	Description string
	Prompt      string
	// Executes says the skills should be used for the prompt; false, that
	// the skill shouldn't be.
	Executes bool
	// ExpectedSkills must all be used when Executes (none: the skill).
	ExpectedSkills []string
	Threshold      float64
	// Outcome is the answer expected, scored by meaning.
	Outcome string
	// Path is its file; Problem why it can't be read or run.
	Path    string
	Problem string
}

// scenarioFront is a scenario file's front matter.
type scenarioFront struct {
	Name           string   `yaml:"name"`
	Description    string   `yaml:"description"`
	Prompt         string   `yaml:"prompt"`
	Executes       *bool    `yaml:"executes"`
	ExpectedSkills []string `yaml:"expected_skills"`
	SkillsApplied  []string `yaml:"skills_applied"`
	Threshold      *float64 `yaml:"threshold"`
}

// Scenarios are s's scenarios, by name: none for a built-in skill (its
// files aren't on disk).
func Scenarios(s *Skill) []Scenario {
	if s == nil || s.hostDir == "" {
		return nil
	}
	return LoadScenarios(filepath.Join(s.hostDir, ScenariosDir))
}

// LoadScenarios reads dir's *.md, by name; one that can't be read or run
// is listed with its problem.
func LoadScenarios(dir string) []Scenario {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Scenario
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		sc := Scenario{Name: strings.TrimSuffix(e.Name(), ".md"), Path: p}
		data, err := os.ReadFile(p)
		if err != nil {
			sc.Problem = err.Error()
		} else {
			sc = parseScenario(sc, data)
		}
		out = append(out, sc)
	}
	slices.SortFunc(out, func(a, b Scenario) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// parseScenario fills sc from its file's content.
func parseScenario(sc Scenario, data []byte) Scenario {
	front, body, ok := frontmatter.Split(data)
	if !ok {
		sc.Problem = "no front matter: it needs at least a prompt"
		return sc
	}
	var f scenarioFront
	if err := yaml.Unmarshal(front, &f); err != nil {
		sc.Problem = "its front matter isn't YAML: " + err.Error()
		return sc
	}
	sc.Description, sc.Prompt = strings.TrimSpace(f.Description), strings.TrimSpace(f.Prompt)
	sc.Executes = f.Executes == nil || *f.Executes
	sc.ExpectedSkills = f.ExpectedSkills
	if len(sc.ExpectedSkills) == 0 {
		sc.ExpectedSkills = f.SkillsApplied
	}
	sc.Threshold = DefaultScenarioThreshold
	if f.Threshold != nil {
		sc.Threshold = *f.Threshold
	}
	sc.Outcome = strings.TrimSpace(string(body))
	if probs := CheckScenario(sc); len(probs) > 0 {
		sc.Problem = strings.Join(probs, "; ")
	}
	return sc
}

// CheckScenario returns what keeps sc from being saved or run.
func CheckScenario(sc Scenario) []string {
	var probs []string
	if !ValidName(sc.Name) {
		probs = append(probs, fmt.Sprintf("name %q: lowercase letters, digits and single hyphens, up to 64", sc.Name))
	}
	if strings.TrimSpace(sc.Prompt) == "" {
		probs = append(probs, "prompt: what to ask the agent")
	}
	if sc.Threshold < 0 || sc.Threshold > 1 {
		probs = append(probs, fmt.Sprintf("threshold %g: between 0 and 1", sc.Threshold))
	}
	if strings.TrimSpace(sc.Outcome) == "" {
		probs = append(probs, "outcome: the answer expected")
	}
	for _, s := range sc.ExpectedSkills {
		if !ValidName(s) {
			probs = append(probs, fmt.Sprintf("expected skills: %q isn't a skill's name", s))
		}
	}
	return probs
}

// SaveScenario writes dir/<skill>/scenarios/<sc.Name>.md, keeping the
// front matter's other fields when it replaces previous ("" for a new
// one; a different name renames it), and returns its path, or the
// problems that keep it from being saved (nothing written).
func SaveScenario(dir, skill string, sc Scenario, previous string) (string, []string, error) {
	sc.Name = strings.TrimSpace(sc.Name)
	if probs := CheckScenario(sc); len(probs) > 0 {
		return "", probs, nil
	}
	if !ValidName(skill) {
		return "", nil, fmt.Errorf("%w: %q", ErrUnknownSkill, skill)
	}
	folder := filepath.Join(dir, skill, ScenariosDir)
	target := filepath.Join(folder, sc.Name+".md")
	var old []byte
	if previous != "" {
		if !ValidName(previous) {
			return "", nil, fmt.Errorf("%w: %q", ErrUnknownScenario, previous)
		}
		data, err := os.ReadFile(filepath.Join(folder, previous+".md"))
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil, fmt.Errorf("%w: %q", ErrUnknownScenario, previous)
		} else if err != nil {
			return "", nil, err
		}
		old = data
	}
	if previous != sc.Name {
		if _, err := os.Stat(target); err == nil {
			return "", []string{fmt.Sprintf("name %q: the skill has a scenario by that name", sc.Name)}, nil
		}
	}
	data, err := frontmatter.Edit(old, strings.TrimSpace(sc.Outcome)+"\n", func(f frontmatter.Fields) {
		f.String("description", strings.TrimSpace(sc.Description))
		f.String("prompt", strings.TrimSpace(sc.Prompt))
		f.Flag("executes", sc.Executes, !sc.Executes) // always written, as Castor's are
		f.List("expected_skills", sc.ExpectedSkills)
		f.List("skills_applied", nil) // expected_skills says it
		f.Float("threshold", sc.Threshold)
	})
	if err != nil {
		return "", nil, fmt.Errorf("the scenario's %w", err)
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", nil, err
	}
	if err := writeAtomic(target, data); err != nil {
		return "", nil, err
	}
	if previous != "" && previous != sc.Name {
		if err := os.Remove(filepath.Join(folder, previous+".md")); err != nil {
			return "", nil, err
		}
	}
	return target, nil, nil
}

// DeleteScenario removes dir/<skill>/scenarios/<name>.md.
func DeleteScenario(dir, skill, name string) error {
	if !ValidName(skill) || !ValidName(name) {
		return fmt.Errorf("%w: %q", ErrUnknownScenario, name)
	}
	err := os.Remove(filepath.Join(dir, skill, ScenariosDir, name+".md"))
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %q", ErrUnknownScenario, name)
	}
	return err
}

// ErrUnknownScenario is a scenario the skill doesn't have.
var ErrUnknownScenario = errors.New("no such scenario")
