// Package tsierr defines the typed errors returned by tsi-cli. Each error names the
// module and step that failed and carries a suggested fix, and maps to a stable CLI exit code.
package tsierr

import (
	"errors"
	"fmt"
)

// Code classifies a failure. The CLI maps codes to exit statuses.
type Code int

const (
	CodeInternal   Code = 1
	CodeUsage      Code = 2
	CodePreflight  Code = 3
	CodeNotReady   Code = 4
	CodeBootstrap  Code = 5
	CodeTestFailed Code = 6
	CodeImmutable  Code = 7
	CodeStateLost  Code = 8
	CodeNotFound   Code = 9
	CodeMigration  Code = 10
)

// Error is a failure with enough context to act on.
type Error struct {
	Code   Code
	Module string
	Step   string
	Msg    string
	Hint   string
	Cause  error
}

func (e *Error) Error() string {
	s := e.Msg
	if e.Module != "" {
		s = e.Module + ": " + s
	}
	if e.Step != "" {
		s = "[" + e.Step + "] " + s
	}
	if e.Cause != nil {
		s += ": " + e.Cause.Error()
	}
	if e.Hint != "" {
		s += "\n  fix: " + e.Hint
	}
	return s
}

func (e *Error) Unwrap() error { return e.Cause }

// New builds an Error.
func New(code Code, module, step, msg, hint string, cause error) *Error {
	return &Error{Code: code, Module: module, Step: step, Msg: msg, Hint: hint, Cause: cause}
}

// Newf is New without a hint or cause.
func Newf(code Code, module, format string, a ...any) *Error {
	return &Error{Code: code, Module: module, Msg: fmt.Sprintf(format, a...)}
}

// ExitCode returns the process exit status for err.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var e *Error
	if errors.As(err, &e) {
		return int(e.Code)
	}
	return int(CodeInternal)
}
