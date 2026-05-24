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
