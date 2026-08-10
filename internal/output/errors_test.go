package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	stripeapi "github.com/stripe/stripe-go/v85"
)

func TestEmitErrorPlain(t *testing.T) {
	var buf bytes.Buffer
	EmitError(&buf, "no such alias", FixableByAgent)
	var env ErrorEnvelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error != "no such alias" || env.FixableBy != FixableByAgent {
		t.Fatalf("envelope mismatch: %+v", env)
	}
	if env.StripeCode != "" || env.HTTPStatus != 0 || env.RequestID != "" {
		t.Fatalf("expected stripe fields empty: %+v", env)
	}
}

// stripeErrorEnvelopeFrom builds the envelope FailFromStripeError would emit,
// without invoking os.Exit. Mirrors the helper's logic so unit tests can
// assert field-by-field on a real *stripe.Error.
func stripeErrorEnvelopeFrom(err error) ErrorEnvelope {
	var se *stripeapi.Error
	if errors.As(err, &se) {
		return ErrorEnvelope{
			Error:      se.Msg,
			FixableBy:  FixableByAgent,
			StripeCode: string(se.Code),
			HTTPStatus: se.HTTPStatusCode,
			RequestID:  se.RequestID,
		}
	}
	return ErrorEnvelope{Error: err.Error(), FixableBy: FixableByAgent}
}

func TestStripeErrorUnpacksStructuredFields(t *testing.T) {
	in := &stripeapi.Error{
		Code:           stripeapi.ErrorCodeResourceMissing,
		HTTPStatusCode: 404,
		Msg:            "No such charge: 'ch_does_not_exist'",
		RequestID:      "req_ABC",
		Type:           stripeapi.ErrorTypeInvalidRequest,
	}
	got := stripeErrorEnvelopeFrom(in)
	if got.Error != in.Msg {
		t.Errorf("error = %q, want %q", got.Error, in.Msg)
	}
	if got.StripeCode != string(in.Code) {
		t.Errorf("stripeCode = %q, want %q", got.StripeCode, in.Code)
	}
	if got.HTTPStatus != 404 {
		t.Errorf("httpStatus = %d, want 404", got.HTTPStatus)
	}
	if got.RequestID != "req_ABC" {
		t.Errorf("requestId = %q, want req_ABC", got.RequestID)
	}
}

func TestStripeErrorWrappedIsUnpacked(t *testing.T) {
	inner := &stripeapi.Error{Code: stripeapi.ErrorCodeResourceMissing, HTTPStatusCode: 404, Msg: "nope"}
	wrapped := fmt.Errorf("retrieving charge: %w", inner)
	got := stripeErrorEnvelopeFrom(wrapped)
	if got.StripeCode != string(stripeapi.ErrorCodeResourceMissing) {
		t.Fatalf("expected wrapped stripe error to unpack, got %+v", got)
	}
	if got.Error != "nope" {
		t.Fatalf("expected human msg, got %q", got.Error)
	}
}

func TestEnvelopeHintMarshalling(t *testing.T) {
	// With a hint set, `hint` appears in JSON.
	b, err := json.Marshal(ErrorEnvelope{Error: "x", FixableBy: FixableByAgent, Hint: "did you mean \"charge\"?"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != `{"error":"x","fixableBy":"agent","hint":"did you mean \"charge\"?"}` {
		t.Errorf("with-hint marshal = %s", got)
	}
	// Empty hint is omitted.
	b, _ = json.Marshal(ErrorEnvelope{Error: "x", FixableBy: FixableByAgent})
	if got := string(b); got != `{"error":"x","fixableBy":"agent"}` {
		t.Errorf("no-hint marshal = %s", got)
	}
}

func TestNonStripeErrorFallsThrough(t *testing.T) {
	got := stripeErrorEnvelopeFrom(errors.New("local validation failed"))
	if got.Error != "local validation failed" {
		t.Fatalf("error = %q", got.Error)
	}
	if got.StripeCode != "" || got.HTTPStatus != 0 || got.RequestID != "" {
		t.Fatalf("expected stripe fields empty for non-stripe error: %+v", got)
	}
}

// account_invalid means the acct_ isn't connected to this platform (or doesn't
// exist). Retrying can never fix that, so it must not be reported as
// agent-fixable.
func TestConnectAccountInvalidIsHumanFixable(t *testing.T) {
	se := &stripeapi.Error{
		Code:           stripeapi.ErrorCodeAccountInvalid,
		HTTPStatusCode: 403,
		Msg:            "The provided key does not have access to account 'acct_x'",
		Type:           stripeapi.ErrorTypeInvalidRequest,
	}
	got := StripeErrorEnvelope(se, FixableByAgent)
	if got.FixableBy != FixableByHuman {
		t.Errorf("fixableBy = %q, want human", got.FixableBy)
	}
	if !strings.Contains(got.Hint, "connected to this platform") {
		t.Errorf("hint should point at the platform connection, got %q", got.Hint)
	}
	if got.StripeCode != string(stripeapi.ErrorCodeAccountInvalid) {
		t.Errorf("stripeCode = %q", got.StripeCode)
	}
}

// A restricted key without the Connect read scope 403s with no specific code.
// The hint must name the missing permission rather than suggest a retry.
func TestConnectPermissionErrorIsHumanFixable(t *testing.T) {
	se := &stripeapi.Error{
		HTTPStatusCode: 403,
		Msg:            "This API key does not have permission to read this resource.",
		Type:           stripeapi.ErrorTypeInvalidRequest,
	}
	got := StripeErrorEnvelope(se, FixableByAgent)
	if got.FixableBy != FixableByHuman {
		t.Errorf("fixableBy = %q, want human", got.FixableBy)
	}
	if !strings.Contains(got.Hint, "restricted key") {
		t.Errorf("hint should name the restricted-key scope, got %q", got.Hint)
	}
}

// Ordinary Stripe errors keep the caller's classification and gain no hint.
func TestNonConnectErrorKeepsAgentFixable(t *testing.T) {
	se := &stripeapi.Error{
		Code:           stripeapi.ErrorCodeResourceMissing,
		HTTPStatusCode: 404,
		Msg:            "No such charge",
	}
	got := StripeErrorEnvelope(se, FixableByAgent)
	if got.FixableBy != FixableByAgent {
		t.Errorf("fixableBy = %q, want agent", got.FixableBy)
	}
	if got.Hint != "" {
		t.Errorf("hint = %q, want empty", got.Hint)
	}
}
