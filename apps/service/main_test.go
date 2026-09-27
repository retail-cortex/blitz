package main

import (
	"context"
	"testing"
)

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"extra"}, {"--nope"}} {
		if code := run(context.Background(), args); code != exitUsage {
			t.Errorf("blitzd %v: exit %d, want %d", args, code, exitUsage)
		}
	}
	if code := run(context.Background(), []string{"--version"}); code != 0 {
		t.Errorf("blitzd --version: exit %d", code)
	}
}
