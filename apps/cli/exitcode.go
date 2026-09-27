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

package main

import (
	"context"
	"errors"

	"github.com/retail-cortex/blitz/pkg/api"
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
	case api.IsLimit(err):
		return exitMaxTurns
	case errors.Is(err, context.Canceled):
		return exitInterrupted
	default:
		return exitFailure
	}
}
