package runtime

import (
	"context"
	"fmt"
	"os"
	"testing"

	"go.uber.org/goleak"
)

// No test may leave a goroutine running. The shared test telemetry lives
// for the whole process, so it is shut down before the check. Tests get a
// home directory of their own: what Blitz keeps under ~/.blitz
// (checkpoints, approvals) must not reach the real one.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "blitz-runtime-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	tel, _ := testTelemetry()
	_ = tel.Shutdown(context.Background())
	if code == 0 {
		if err := goleak.Find(); err != nil {
			fmt.Fprintf(os.Stderr, "goleak: %v\n", err)
			code = 1
		}
	}
	os.Exit(code)
}
