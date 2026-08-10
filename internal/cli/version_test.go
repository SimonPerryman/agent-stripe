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

func TestResolveAPIVersion_Precedence(t *testing.T) {
	t.Run("flag wins over env", func(t *testing.T) {
		t.Setenv("AGENT_STRIPE_API_VERSION", "2020-08-27")
		if got := resolveAPIVersion("2022-11-15"); got != "2022-11-15" {
			t.Errorf("got %q, want 2022-11-15", got)
		}
	})
	t.Run("env used when flag empty", func(t *testing.T) {
		t.Setenv("AGENT_STRIPE_API_VERSION", "2020-08-27")
		if got := resolveAPIVersion(""); got != "2020-08-27" {
			t.Errorf("got %q, want 2020-08-27", got)
		}
	})
	t.Run("empty when neither set", func(t *testing.T) {
		t.Setenv("AGENT_STRIPE_API_VERSION", "")
		if got := resolveAPIVersion(""); got != "" {
			t.Errorf("got %q, want empty (the pinned version)", got)
		}
	})
}

func TestValidateAPIVersion(t *testing.T) {
	for _, v := range []string{"", "2022-11-15", "2020-08-27", "2026-04-22.dahlia", agentstripe.PinnedAPIVersion} {
		if err := ValidateAPIVersion(v); err != nil {
			t.Errorf("ValidateAPIVersion(%q) = %v, want nil", v, err)
		}
	}

	// Stripe answers an unknown version with a 400 that does not name the
	// offending parameter, so shape mistakes have to be caught locally.
	for _, v := range []string{"2022-11-5", "22-11-15", "latest", "2022/11/15", "2022-11-15.", "v1"} {
		err := ValidateAPIVersion(v)
		if err == nil {
			t.Errorf("ValidateAPIVersion(%q) = nil, want error", v)
			continue
		}
		var oe *output.Error
		if !errors.As(err, &oe) {
			t.Errorf("ValidateAPIVersion(%q): want *output.Error, got %T", v, err)
			continue
		}
		if oe.By != output.FixableByAgent {
			t.Errorf("ValidateAPIVersion(%q): fixableBy = %q, want agent", v, oe.By)
		}
		if oe.Hint == "" {
			t.Errorf("ValidateAPIVersion(%q): expected a hint an agent can act on", v)
		}
	}
}

func TestGlobalsFlagSet_APIVersionAndRaw(t *testing.T) {
	fs, g := newGlobalFlags()
	fs.SetOutput(new(strings.Builder))
	if err := fs.Parse([]string{"--raw", "--api-version", "2022-11-15", "invoice", "get", "in_1"}); err != nil {
		t.Fatal(err)
	}
	if !*g.raw {
		t.Error("--raw did not parse")
	}
	if *g.apiVersion != "2022-11-15" {
		t.Errorf("--api-version = %q, want 2022-11-15", *g.apiVersion)
	}
	if fs.Arg(0) != "invoice" {
		t.Errorf("first positional = %q, want invoice", fs.Arg(0))
	}
}

// The envelope's apiVersion must report the version actually requested. Under
// an override, echoing the pinned constant would be a lie exactly where a
// reader is trying to establish which shape they are looking at.
func TestEffectiveAPIVersion(t *testing.T) {
	if got := EffectiveAPIVersion(&GlobalOpts{}); got != agentstripe.PinnedAPIVersion {
		t.Errorf("no override: got %q, want the pinned %q", got, agentstripe.PinnedAPIVersion)
	}
	if got := EffectiveAPIVersion(&GlobalOpts{APIVersion: "2022-11-15"}); got != "2022-11-15" {
		t.Errorf("override: got %q, want 2022-11-15", got)
	}
}

func TestEnvelopeFor_ReportsVersionAndRaw(t *testing.T) {
	env := EnvelopeFor(&GlobalOpts{APIVersion: "2022-11-15", Raw: true})
	if env.APIVersion != "2022-11-15" {
		t.Errorf("apiVersion = %q, want 2022-11-15", env.APIVersion)
	}
	if !env.Raw {
		t.Error("raw marker missing — raw output is a different contract and must say so")
	}

	// A default run stays byte-identical to before these flags existed.
	b, err := json.Marshal(EnvelopeFor(&GlobalOpts{}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"raw"`) {
		t.Errorf("raw should be omitted when unset, got %s", b)
	}
	if !strings.Contains(string(b), agentstripe.PinnedAPIVersion) {
		t.Errorf("expected the pinned version in a default envelope, got %s", b)
	}
}

// The NDJSON header is the only place a streamed response can declare its
// version and rawness, so it must carry both.
func TestStreamHeaderCarriesVersionAndRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"object":"list","has_more":false,"url":"/v1/invoices",`+
			`"data":[{"id":"in_1","object":"invoice","application_fee_amount":100}]}`)
	}))
	defer srv.Close()

	opts := &GlobalOpts{
		Account:    &config.Account{Alias: "test", Mode: config.ModeTest},
		Stream:     true,
		Raw:        true,
		APIVersion: "2022-11-15",
		Client: agentstripe.NewClient("sk_test_fake", srv.URL, "", 5*time.Second,
			agentstripe.WithAPIVersion("2022-11-15")),
	}
	out := captureCLIStdout(t, func() {
		list := opts.Client.V1Invoices.List(t.Context(), &stripeapi.InvoiceListParams{})
		if err := StreamList(t.Context(), opts, list, 0); err != nil {
			t.Errorf("StreamList: %v", err)
		}
	})

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected a header line and one record, got %d lines: %q", len(lines), out)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &env); err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if env["apiVersion"] != "2022-11-15" {
		t.Errorf("header apiVersion = %v, want 2022-11-15", env["apiVersion"])
	}
	if env["raw"] != true {
		t.Errorf("header raw = %v, want true", env["raw"])
	}

	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &rec); err != nil {
		t.Fatalf("decode record: %v", err)
	}
	if rec["application_fee_amount"] != float64(100) {
		t.Errorf("streamed record lost a raw-only field: %v", rec)
	}
}
