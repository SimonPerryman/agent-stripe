package output

import (
	"encoding/json"
	"io"
	"os"
)

// FixableBy categorizes who can fix an error.
type FixableBy string

const (
	FixableByHuman FixableBy = "human"
	FixableByAgent FixableBy = "agent"
	FixableByRetry FixableBy = "retry"
)

// ErrorEnvelope is the stderr error contract.
type ErrorEnvelope struct {
	Error     string    `json:"error"`
	FixableBy FixableBy `json:"fixableBy,omitempty"`
}

// EmitError writes the error envelope to w (typically os.Stderr) as a single line.
func EmitError(w io.Writer, msg string, by FixableBy) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(ErrorEnvelope{Error: msg, FixableBy: by})
}

// Fail writes the error envelope to stderr and exits with the given code.
func Fail(msg string, by FixableBy, code int) {
	EmitError(os.Stderr, msg, by)
	os.Exit(code)
}
