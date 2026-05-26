package output

import (
	"encoding/json"
	"errors"
	"io"
	"os"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// FixableBy categorizes who can fix an error.
type FixableBy string

const (
	FixableByHuman FixableBy = "human"
	FixableByAgent FixableBy = "agent"
	FixableByRetry FixableBy = "retry"
)

// ErrorEnvelope is the stderr error contract. Stripe-specific fields are
// optional and only populated when the underlying error is a *stripe.Error;
// readers that only know about `error` + `fixableBy` keep working unchanged.
type ErrorEnvelope struct {
	Error      string    `json:"error"`
	FixableBy  FixableBy `json:"fixableBy,omitempty"`
	Hint       string    `json:"hint,omitempty"`
	StripeCode string    `json:"stripeCode,omitempty"`
	HTTPStatus int       `json:"httpStatus,omitempty"`
	RequestID  string    `json:"requestId,omitempty"`
}

// EmitError writes the error envelope to w (typically os.Stderr) as a single line.
func EmitError(w io.Writer, msg string, by FixableBy) {
	emit(w, ErrorEnvelope{Error: msg, FixableBy: by})
}

// Fail writes the error envelope to stderr and exits with the given code.
func Fail(msg string, by FixableBy, code int) {
	EmitError(os.Stderr, msg, by)
	os.Exit(code)
}

// FailWithHint is Fail plus a `hint` field carrying a closest-match suggestion
// (or "valid: a, b, c" fallback) so an agent can self-correct without a human
// round-trip. Empty hint behaves identically to Fail.
func FailWithHint(msg, hint string, by FixableBy, code int) {
	emit(os.Stderr, ErrorEnvelope{Error: msg, FixableBy: by, Hint: hint})
	os.Exit(code)
}

// Error is a sentinel that command packages return when they want the
// dispatcher to emit a hint without knowing the exit code. The dispatcher
// detects it via errors.As and routes to FailWithHint.
type Error struct {
	Msg  string
	Hint string
	By   FixableBy
}

func (e *Error) Error() string { return e.Msg }

// FailFromStripeError unpacks a *stripe.Error into the structured envelope
// (human message in `error`, code/status/request-id in their own fields) and
// exits. Non-Stripe errors fall through to plain Fail so the contract for
// non-API errors is unchanged.
func FailFromStripeError(err error, by FixableBy, code int) {
	var se *stripeapi.Error
	if errors.As(err, &se) {
		emit(os.Stderr, ErrorEnvelope{
			Error:      se.Msg,
			FixableBy:  by,
			StripeCode: string(se.Code),
			HTTPStatus: se.HTTPStatusCode,
			RequestID:  se.RequestID,
		})
		os.Exit(code)
	}
	Fail(err.Error(), by, code)
}

func emit(w io.Writer, env ErrorEnvelope) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(env)
}
