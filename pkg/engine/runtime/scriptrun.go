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

package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/redact"
)

// Credential scripts (spec_models_015 MDL-55, MDL-102): api_key_command
// and the api provider's token script print one line, a key or a token.
// They run with the service's environment, outside the agent's sandbox:
// they're the user's own settings, never a project's.

// maxScriptStderr is how much of a failed script's standard error an error
// quotes.
const maxScriptStderr = 256

// scriptTimeout bounds one run of a credential script; a variable so tests
// can shorten it.
var scriptTimeout = 30 * time.Second

// lookPath finds an interpreter; a variable so tests can hide one.
var lookPath = exec.LookPath

// runScript runs src, written in lang (config.ScriptShell, ScriptPython or
// ScriptTypeScript), and returns the one line it prints, trimmed.
func runScript(ctx context.Context, lang, src string) (string, error) {
	argv, cleanup, err := scriptCommand(lang, src)
	if err != nil {
		return "", err
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(ctx, scriptTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("timed out after %s", scriptTimeout)
		}
		return "", fmt.Errorf("failed: %v: %s", err, scriptStderr(stderr.String()))
	}
	line := strings.TrimSpace(out.String())
	if line == "" || strings.ContainsAny(line, "\r\n") {
		return "", errors.New("printed nothing (or more than one line)")
	}
	return line, nil
}

// scriptCommand is the command that runs src in lang, and what removes
// any file it needed.
func scriptCommand(lang, src string) (argv []string, cleanup func(), err error) {
	cleanup = func() {}
	switch lang {
	case config.ScriptShell, "":
		if _, err := os.Stat("/bin/sh"); err != nil {
			return nil, cleanup, errors.New("shell scripts need /bin/sh (macOS and Linux)")
		}
		return []string{"/bin/sh", "-c", src}, cleanup, nil
	case config.ScriptPython:
		python, err := lookPath("python3")
		if err != nil {
			return nil, cleanup, errors.New("python scripts need python3 on the service's PATH")
		}
		return []string{python, "-c", src}, cleanup, nil
	case config.ScriptTypeScript:
		node, err := lookPath("node")
		if err != nil {
			return nil, cleanup, errors.New("typescript scripts need node (22.6 or later) on the service's PATH")
		}
		// node strips types only from files, so the script is written to one.
		dir, err := os.MkdirTemp("", "blitz-token-*")
		if err != nil {
			return nil, cleanup, err
		}
		cleanup = func() { _ = os.RemoveAll(dir) }
		file := filepath.Join(dir, "token.ts")
		if err := os.WriteFile(file, []byte(src), 0o600); err != nil {
			cleanup()
			return nil, func() {}, err
		}
		return append(append([]string{node}, tools.NodeTypeScriptFlags...), file), cleanup, nil
	}
	return nil, cleanup, fmt.Errorf("unknown script language %q", lang)
}

// scriptStderr is a failed script's standard error (or a gateway's error
// message) as an error may quote it: with secrets (remembered tokens, known key formats) masked first, so
// cutting can't split one past recognition, then trimmed and cut short.
func scriptStderr(s string) string {
	s = strings.TrimSpace(redact.New().String(s))
	if r := []rune(s); len(r) > maxScriptStderr {
		s = string(r[:maxScriptStderr]) + "…"
	}
	return s
}
