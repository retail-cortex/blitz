package tui

import (
	"context"
	"strings"
	"testing"
)

func TestPermissionsCommand(t *testing.T) {
	app, _ := newCommandApp(t, "/permissions\n/permissions deny Bash(git push *)\n/permissions ask\n/permissions deny what(x)\n/permissions\n/permissions remove shell(git push *)\n/permissions remove shell(nope)\n/exit\n")
	out := captureStdout(t, func() { RunREPL(context.Background(), app) })
	for _, want := range []string{
		"Permission rules", "none — add some",
		"deny shell(git push *) (this session)",
		"Usage: /permissions",
		"unknown kind",
		"Removed shell(git push *)",
		"No rule shell(nope)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
