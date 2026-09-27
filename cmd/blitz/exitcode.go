package main

import (
	"context"
	"errors"

	"github.com/retail-cortex/blitz/internal/app"
)

// Exit codes are stable so scripts can react to them.
const (
	exitOK          = 0
	exitFailure     = 1   // runtime or model error
	exitUsage       = 2   // invalid flags or arguments
	exitMaxTurns    = 3   // a limit (--max-turns, --max-cost-usd, --timeout) stopped the run
	exitBlocked     = 4   // a prompt_submit hook blocked the prompt
	exitInterrupted = 130 // Ctrl+C / SIGINT
)

// exitError carries a specific exit code.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func withCode(code int, err error) error {
	if err == nil {
		return nil
	}
	return &exitError{code: code, err: err}
}

// exitCodeFor maps an error to the process exit code.
func exitCodeFor(err error) int {
	var ee *exitError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &ee):
		return ee.code
	case app.IsLimit(err):
		return exitMaxTurns
	case errors.Is(err, context.Canceled):
		return exitInterrupted
	default:
		return exitFailure
	}
}
