package cli

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/simonperryman/agent-stripe/internal/config"
	"github.com/simonperryman/agent-stripe/internal/output"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"

	stripeapi "github.com/stripe/stripe-go/v85"
)

func TestResolveStripeAccount_Precedence(t *testing.T) {
	t.Run("flag wins over env", func(t *testing.T) {
		t.Setenv("AGENT_STRIPE_STRIPE_ACCOUNT", "acct_env")
		if got := resolveStripeAccount("acct_flag"); got != "acct_flag" {
			t.Errorf("got %q, want acct_flag", got)
		}
	})
	t.Run("env used when flag empty", func(t *testing.T) {
		t.Setenv("AGENT_STRIPE_STRIPE_ACCOUNT", "acct_env")
		if got := resolveStripeAccount(""); got != "acct_env" {
			t.Errorf("got %q, want acct_env", got)
		}
	})
	t.Run("empty when neither set", func(t *testing.T) {
		t.Setenv("AGENT_STRIPE_STRIPE_ACCOUNT", "")
		if got := resolveStripeAccount(""); got != "" {
			t.Errorf("got %q, want empty (platform account)", got)
		}
	})
}

func TestValidateStripeAccount(t *testing.T) {
	valid := []string{"", "acct_1A2b3C", "acct_1234567890"}
	for _, v := range valid {
		if err := ValidateStripeAccount(v); err != nil {
			t.Errorf("ValidateStripeAccount(%q) = %v, want nil", v, err)
		}
	}

	// The obvious agent mistake: an id of the wrong type. Must be caught
	// locally rather than surfacing as an opaque 403 from Stripe.
	for _, v := range []string{"cus_123", "ch_123", "prod", "acct_", "acct_1 2"} {
		err := ValidateStripeAccount(v)
		if err == nil {
			t.Errorf("ValidateStripeAccount(%q) = nil, want error", v)
			continue
		}
		var oe *output.Error
		if !errors.As(err, &oe) {
			t.Errorf("ValidateStripeAccount(%q): want *output.Error, got %T", v, err)
			continue
		}
		if oe.By != output.FixableByAgent {
			t.Errorf("ValidateStripeAccount(%q): fixableBy = %q, want agent", v, oe.By)
		}
		if !strings.Contains(oe.Hint, "acct_") {
			t.Errorf("ValidateStripeAccount(%q): hint should name the acct_ prefix, got %q", v, oe.Hint)
		}
	}
}

func TestGlobalsFlagSet_StripeAccount(t *testing.T) {
	fs, _ := newGlobalsFlagSet()
	if err := fs.Parse([]string{"--stripe-account", "acct_123", "charge", "get", "ch_1"}); err != nil {
		t.Fatal(err)
	}
	if got := fs.Lookup("stripe-account").Value.String(); got != "acct_123" {
		t.Errorf("stripe-account = %q, want acct_123", got)
	}
	if got := fs.Arg(0); got != "charge" {
		t.Errorf("first positional = %q, want charge", got)
	}
}

func TestEnvelopeFor_CarriesStripeAccount(t *testing.T) {
	opts := &GlobalOpts{StripeAccount: "acct_123"}
	if got := EnvelopeFor(opts).StripeAccount; got != "acct_123" {
		t.Errorf("envelope.StripeAccount = %q, want acct_123", got)
	}
	// Platform-scoped runs stay byte-identical to before this flag existed.
	if got := EnvelopeFor(&GlobalOpts{}).StripeAccount; got != "" {
		t.Errorf("envelope.StripeAccount = %q, want empty", got)
	}
}

// The NDJSON header line is the only place a streamed response can say which
// account it read, so it must carry the echo too.
func TestStreamHeaderCarriesStripeAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"cus_1","object":"customer"}],"has_more":false,"url":"/v1/customers"}`)
	}))
	defer srv.Close()

	opts := &GlobalOpts{
		Account:       &config.Account{Alias: "test", Mode: config.ModeTest},
		StripeAccount: "acct_123",
		Stream:        true,
		Client:        agentstripe.NewClient("sk_test_fake", srv.URL, "acct_123", 5*time.Second),
	}
	out := captureCLIStdout(t, func() {
		list := opts.Client.V1Customers.List(t.Context(), &stripeapi.CustomerListParams{})
		if err := StreamList(t.Context(), opts, list, 0); err != nil {
			t.Errorf("StreamList: %v", err)
		}
	})

	header := strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]
	var env map[string]any
	if err := json.Unmarshal([]byte(header), &env); err != nil {
		t.Fatalf("decode header line: %v\nline: %s", err, header)
	}
	if env["stripeAccount"] != "acct_123" {
		t.Fatalf("header stripeAccount = %v, want acct_123 (line: %s)", env["stripeAccount"], header)
	}
}
