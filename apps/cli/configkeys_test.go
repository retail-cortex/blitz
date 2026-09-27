package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/secrets"
)

func TestConfigKeyCommands(t *testing.T) {
	isolate(t)
	store := &secrets.Memory{}
	secrets.SetDefault(store)
	run := func(stdin string, args ...string) (string, error) {
		t.Helper()
		cmd := newRootCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(strings.NewReader(stdin))
		cmd.SetArgs(args)
		err := cmd.Execute()
		return out.String(), err
	}

	if _, err := run("sk-ant-cli\n", "config", "set-key", "anthropic"); err != nil {
		t.Fatal(err)
	}
	if v, _ := store.Get("global/llm.anthropic.api_key"); v != "sk-ant-cli" {
		t.Errorf("stored %q", v)
	}
	out, err := run("", "config", "keys")
	if err != nil || !strings.Contains(out, "anthropic  keychain") || strings.Contains(out, "sk-ant-cli") {
		t.Errorf("keys:\n%s %v", out, err)
	}

	// The workspace's own key, for the current directory.
	if _, err := run("AIza-ws\n", "config", "set-key", "--workspace", "gemini"); err != nil {
		t.Fatal(err)
	}
	out, _ = run("", "config", "keys", "-w")
	if !strings.Contains(out, "gemini     keychain") || !strings.Contains(out, "anthropic  inherited") {
		t.Errorf("workspace keys:\n%s", out)
	}
	if _, err := run("", "config", "remove-key", "-w", "gemini"); err != nil {
		t.Fatal(err)
	}
	if out, _ = run("", "config", "keys", "-w"); !strings.Contains(out, "gemini     none") {
		t.Errorf("after remove:\n%s", out)
	}

	for name, args := range map[string][]string{
		"no key":       {"config", "set-key", "openai"},
		"not keyed":    {"config", "set-key", "ollama"},
		"nothing here": {"config", "secure-key", "openai"},
	} {
		if _, err := run("", args...); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
