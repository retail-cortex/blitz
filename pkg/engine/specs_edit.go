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
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/retail-cortex/blitz/pkg/engine/atomicfile"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
)

var specBulletCheckbox = regexp.MustCompile(`^(\s*(?:[-*]|\d+\.)\s+)(?:\[[ xX]\]\s+)?(.*)$`)

// ApplySpecUpdate applies an atomic update to .agents/specs/<name>.md, or
// creates the next numbered stage when u.Name is empty and u.Title is set.
func (w *Workspace) ApplySpecUpdate(u SpecUpdate) (SpecStage, error) {
	summary := w.Specs()
	known, knownNames, byNum := make(map[string]SpecStage, len(summary.Stages)), make(map[string]bool, len(summary.Stages)), make(map[string]string, len(summary.Stages))
	maxNum := 0
	for _, s := range summary.Stages {
		known[s.Name], knownNames[s.Name] = s, true
		if _, ok := byNum[s.Number]; !ok {
			byNum[s.Number] = s.Name
		}
		if n, err := strconv.Atoi(s.Number); err == nil && n > maxNum {
			maxNum = n
		}
	}
	name := strings.TrimSuffix(strings.TrimSpace(u.Name), ".md")
	if name == "" {
		return w.createSpecStage(u, maxNum+1, knownNames, byNum)
	}
	if !specFilePattern.MatchString(name + ".md") {
		return SpecStage{}, ErrUnknownSpec
	}
	cur, ok := known[name]
	if !ok {
		return SpecStage{}, ErrUnknownSpec
	}
	path := filepath.Join(w.ProjectSpecsDir(), name+".md")
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return SpecStage{}, ErrUnknownSpec
	}
	if u.Hash != "" && skills.FileHash(rawBytes) != u.Hash {
		return SpecStage{}, ErrSpecChanged
	}
	text := string(rawBytes)
	if strings.TrimSpace(u.Content) != "" {
		text = ensureStageTitle(u.Content, cur.Number, cur.Title, u.Title)
	}
	setStatus := strings.TrimSpace(u.Status) != ""
	status := strings.ToLower(strings.TrimSpace(u.Status))
	if setStatus && !slices.Contains(SpecStatuses, status) {
		return SpecStage{}, ErrBadSpecStatus
	}
	cleanDeps := cur.DependsOn
	if u.SetDependsOn {
		if cleanDeps, err = validateSpecDeps(u.DependsOn, name, knownNames, byNum); err != nil {
			return SpecStage{}, err
		}
	} else if strings.TrimSpace(u.Content) != "" {
		parsed := ParseSpecStage(name, cur.Path, text)
		cleanDeps = resolveSpecDeps(parsed.DependsOn, name, knownNames, byNum)
	}
	if hasSpecCycle(name, cleanDeps, known) {
		return SpecStage{}, ErrSpecCycle
	}
	if id := strings.TrimSpace(u.ToggleItemID); id != "" {
		text = toggleSpecItemLine(text, id, u.ItemDone)
	}
	if u.ToggleDoneWhenIndex > 0 {
		text = toggleDoneWhenLine(text, u.ToggleDoneWhenIndex, u.DoneWhenDone)
	}
	if req := strings.TrimSpace(u.AddRequirement); req != "" {
		text = appendRequirementLine(text, name, req)
	}
	if dw := strings.TrimSpace(u.AddDoneWhen); dw != "" {
		text = appendSectionBullet(text, "Done when", "- [ ] "+dw)
	}
	if oq := strings.TrimSpace(u.AddOpenQuestion); oq != "" {
		text = appendSectionBullet(text, "Open questions", "- "+oq)
	}
	if u.RemoveOpenQuestionIndex > 0 {
		text = removeOpenQuestionLine(text, u.RemoveOpenQuestionIndex)
	}
	updated := rewriteSpecHeaderFields(text, setStatus, status, u.SetDependsOn, cleanDeps)
	if err := atomicfile.Write(path, []byte(updated), 0o644); err != nil {
		return SpecStage{}, err
	}
	return w.specByName(name), nil
}

func (w *Workspace) specByName(name string) SpecStage {
	for _, s := range w.Specs().Stages {
		if s.Name == name {
			return s
		}
	}
	return SpecStage{}
}

func (w *Workspace) createSpecStage(u SpecUpdate, nextNum int, knownNames map[string]bool, byNum map[string]string) (SpecStage, error) {
	title := strings.TrimSpace(u.Title)
	if title == "" {
		return SpecStage{}, ErrBadSpecTitle
	}
	nextNum = min(999, max(1, nextNum))
	numStr := fmt.Sprintf("%03d", nextNum)
	slug := slugifyStageTitle(title)
	stem := numStr + "-" + slug
	for n := nextNum; knownNames[stem] && n < 999; {
		n++
		numStr = fmt.Sprintf("%03d", n)
		stem = numStr + "-" + slug
	}
	cleanDeps, err := validateSpecDeps(u.DependsOn, stem, knownNames, byNum)
	if err != nil {
		return SpecStage{}, err
	}
	status := cmp.Or(strings.ToLower(strings.TrimSpace(u.Status)), "draft")
	if !slices.Contains(SpecStatuses, status) {
		return SpecStage{}, ErrBadSpecStatus
	}
	body := strings.TrimSpace(u.Content)
	if body == "" {
		ctx := cmp.Or(strings.TrimSpace(u.Context), "Describe why this stage exists and what it delivers.")
		prefix := stageIDPrefix(slug)
		reqText := cmp.Or(strings.TrimSpace(u.AddRequirement), fmt.Sprintf("Define the primary deliverable for %s.", title))
		dwText := cmp.Or(strings.TrimSpace(u.AddDoneWhen), "All requirements in this stage are verified by automated tests.")
		body = fmt.Sprintf("# %s. %s\n\nStatus: %s\nDepends on: none\n\n## Context\n\n%s\n\n## Goals\n\n- Deliver %s.\n\n## Non-goals\n\n- None.\n\n## Requirements\n\n- [ ] **%s-01** %s\n\n## Done when\n\n- [ ] %s\n\n## Open questions\n\n- None.\n", numStr, title, status, ctx, title, prefix, reqText, dwText)
	} else {
		body = ensureStageTitle(body, numStr, title, title)
	}
	updated := rewriteSpecHeaderFields(body, true, status, true, cleanDeps)
	dir := w.ProjectSpecsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return SpecStage{}, err
	}
	if err := atomicfile.Write(filepath.Join(dir, stem+".md"), []byte(updated), 0o644); err != nil {
		return SpecStage{}, err
	}
	appendStageToReadme(filepath.Join(dir, "README.md"), stem, title, u.Context)
	return w.specByName(stem), nil
}

func slugifyStageTitle(title string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return cmp.Or(strings.Trim(b.String(), "-"), "stage")
}

func stageIDPrefix(slug string) string {
	if clean := strings.ReplaceAll(strings.ToUpper(slug), "-", ""); len(clean) >= 3 {
		return clean[:3]
	}
	return "REQ"
}

func ensureStageTitle(content, num, fallbackTitle, overrideTitle string) string {
	title := cmp.Or(strings.TrimSpace(overrideTitle), fallbackTitle)
	trimmed := strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
	if strings.HasPrefix(trimmed, "# ") {
		return trimmed + "\n"
	}
	return fmt.Sprintf("# %s. %s\n\n%s\n", num, title, trimmed)
}

func validateSpecDeps(deps []string, self string, known map[string]bool, byNum map[string]string) ([]string, error) {
	var out []string
	for _, dep := range deps {
		if isNoneText(dep) {
			continue
		}
		resolved := resolveOneSpecDep(dep, known, byNum)
		if resolved == "" || resolved == self {
			return nil, fmt.Errorf("%w: %s", ErrUnknownSpec, dep)
		}
		if !slices.Contains(out, resolved) {
			out = append(out, resolved)
		}
	}
	slices.Sort(out)
	return out, nil
}

func setLineCheckbox(line string, done bool) string {
	box := "[ ]"
	if done {
		box = "[x]"
	}
	if m := specBulletCheckbox.FindStringSubmatch(line); m != nil {
		return m[1] + box + " " + m[2]
	}
	return fmt.Sprintf("- %s %s", box, strings.TrimSpace(line))
}

func toggleSpecItemLine(text, itemID string, done bool) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if m := specItemPattern.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			if id := cmp.Or(m[2], m[3]); strings.EqualFold(id, itemID) {
				lines[i] = setLineCheckbox(line, done)
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}

func toggleDoneWhenLine(text string, targetIndex int, done bool) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	inDoneWhen, seen := false, 0
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "## ") {
			sec := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(t, "## ")))
			inDoneWhen = strings.HasPrefix(sec, "done when") || strings.HasPrefix(sec, "acceptance")
			continue
		}
		if inDoneWhen {
			if bullet, ok, _ := trimBullet(t); ok && !isNoneText(bullet) {
				if seen++; seen == targetIndex {
					lines[i] = setLineCheckbox(line, done)
					break
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

func appendRequirementLine(text, stageName, reqText string) string {
	parsed := ParseSpecStage(stageName, "", text)
	prefix := "REQ"
	if i := strings.IndexByte(stageName, '-'); i == 3 && len(stageName) > 4 {
		prefix = stageIDPrefix(stageName[4:])
	}
	maxIdx := 0
	for _, it := range parsed.Items {
		if dash := strings.LastIndexByte(it.ID, '-'); dash > 0 {
			prefix = it.ID[:dash]
			if n, err := strconv.Atoi(strings.TrimRight(it.ID[dash+1:], "abcdefghijklmnopqrstuvwxyz")); err == nil && n > maxIdx {
				maxIdx = n
			}
		}
	}
	return appendSectionBullet(text, "Requirements", fmt.Sprintf("- [ ] **%s-%02d** %s", prefix, maxIdx+1, reqText))
}

func appendSectionBullet(text, defaultHeading, bulletLine string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	targetLower := strings.ToLower(defaultHeading)
	secStart, secEnd := -1, len(lines)
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "## ") {
			continue
		}
		sec := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(t, "## ")))
		if secStart >= 0 {
			secEnd = i
			break
		}
		if (targetLower == "requirements" && (sec == "requirements" || sec == "objectives" || sec == "content")) || strings.HasPrefix(sec, targetLower) {
			secStart = i
		}
	}
	if secStart < 0 {
		return fmt.Sprintf("%s\n\n## %s\n\n%s\n", strings.TrimRight(strings.Join(lines, "\n"), "\n"), defaultHeading, bulletLine)
	}
	for i := secStart + 1; i < secEnd; i++ {
		if b, ok, _ := trimBullet(strings.TrimSpace(lines[i])); ok && isNoneText(b) {
			lines[i] = bulletLine
			return strings.Join(lines, "\n")
		}
	}
	insertAt := secEnd
	for insertAt > secStart+1 && strings.TrimSpace(lines[insertAt-1]) == "" {
		insertAt--
	}
	return strings.Join(slices.Insert(lines, insertAt, bulletLine), "\n")
}

func removeOpenQuestionLine(text string, targetIndex int) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	inOQ, seen, removeIdx, count := false, 0, -1, 0
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "## ") {
			inOQ = strings.HasPrefix(strings.ToLower(strings.TrimSpace(strings.TrimPrefix(t, "## "))), "open question")
			continue
		}
		if inOQ {
			if b, ok, _ := trimBullet(t); ok && !isNoneText(b) {
				seen++
				count++
				if seen == targetIndex {
					removeIdx = i
				}
			}
		}
	}
	if removeIdx >= 0 {
		if count == 1 {
			lines[removeIdx] = "- None."
		} else {
			lines = slices.Delete(lines, removeIdx, removeIdx+1)
		}
	}
	return strings.Join(lines, "\n")
}

func rewriteSpecHeaderFields(text string, setStatus bool, status string, setDeps bool, dependsOn []string) string {
	if !setStatus && !setDeps {
		return text
	}
	depValue := cmp.Or(strings.Join(dependsOn, ", "), "none")
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	endHeader, titleIdx, statusIdx, depIdx := len(lines), -1, -1, -1
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "## ") {
			endHeader = i
			break
		}
		if strings.HasPrefix(t, "# ") && titleIdx < 0 {
			titleIdx = i
			continue
		}
		clean := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(strings.TrimPrefix(t, "- "), "* "), "**", ""))
		if strings.HasPrefix(clean, "status:") && statusIdx < 0 {
			statusIdx = i
		} else if strings.HasPrefix(clean, "depends on:") && depIdx < 0 {
			depIdx = i
		}
	}
	if setStatus && statusIdx >= 0 {
		lines[statusIdx] = "Status: " + status
	}
	if setDeps && depIdx >= 0 {
		lines[depIdx] = "Depends on: " + depValue
	}
	var inject []string
	if setStatus && statusIdx < 0 {
		inject = append(inject, "Status: "+status)
	}
	if setDeps && depIdx < 0 {
		inject = append(inject, "Depends on: "+depValue)
	}
	if len(inject) > 0 {
		lines = slices.Insert(lines, max(0, min(titleIdx+1, endHeader)), inject...)
	}
	return strings.Join(lines, "\n")
}

func appendStageToReadme(readmePath, stem, title, summary string) {
	line := fmt.Sprintf("- [%s](%s.md) — %s", stem, stem, title)
	if s := strings.TrimSpace(summary); s != "" {
		line = fmt.Sprintf("- [%s](%s.md) — %s: %s", stem, stem, title, s)
	}
	raw, err := os.ReadFile(readmePath)
	if err != nil {
		_ = atomicfile.Write(readmePath, []byte(fmt.Sprintf("# Specifications\n\nProject specification stages in execution order.\n\n%s\n", line)), 0o644)
		return
	}
	if text := strings.TrimRight(string(raw), "\n"); !strings.Contains(text, stem+".md") {
		_ = atomicfile.Write(readmePath, []byte(text+"\n"+line+"\n"), 0o644)
	}
}
