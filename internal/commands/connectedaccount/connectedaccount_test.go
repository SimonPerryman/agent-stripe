package connectedaccount

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/simonperryman/agent-stripe/internal/output"
	"github.com/simonperryman/agent-stripe/internal/testutil"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// recordingServer captures the path and query of the last request so tests can
// assert filter passthrough without decoding the response.
func recordingServer(t *testing.T, body string) (*httptest.Server, *http.Request) {
	t.Helper()
	var last http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last = *r
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &last
}

func TestGet_Smoke(t *testing.T) {
	srv, last := recordingServer(t, `{"id":"acct_1","object":"account","charges_enabled":true}`)
	testutil.CaptureStdout(t)
	if err := runGet(context.Background(), testutil.NewOpts(srv.URL), []string{"acct_1"}); err != nil {
		t.Fatalf("runGet: %v", err)
	}
	if last.URL.Path != "/v1/accounts/acct_1" {
		t.Errorf("path = %q, want /v1/accounts/acct_1", last.URL.Path)
	}
}

func TestList_Smoke(t *testing.T) {
	srv, last := recordingServer(t, `{"object":"list","data":[{"id":"acct_1","object":"account"}],"has_more":false,"url":"/v1/accounts"}`)
	testutil.CaptureStdout(t)
	if err := runList(context.Background(), testutil.NewOpts(srv.URL), []string{"--limit", "3", "--created-gt", "100"}); err != nil {
		t.Fatalf("runList: %v", err)
	}
	q := last.URL.Query()
	if q.Get("limit") != "3" {
		t.Errorf("limit = %q, want 3", q.Get("limit"))
	}
	if q.Get("created[gt]") != "100" {
		t.Errorf("created[gt] = %q, want 100", q.Get("created[gt]"))
	}
}

func TestCapabilities_PathIncludesAccount(t *testing.T) {
	srv, last := recordingServer(t, `{"object":"list","data":[{"id":"card_payments","object":"capability","status":"active"}],"has_more":false,"url":"/v1/accounts/acct_1/capabilities"}`)
	testutil.CaptureStdout(t)
	if err := runCapabilities(context.Background(), testutil.NewOpts(srv.URL), []string{"acct_1"}); err != nil {
		t.Fatalf("runCapabilities: %v", err)
	}
	if last.URL.Path != "/v1/accounts/acct_1/capabilities" {
		t.Errorf("path = %q", last.URL.Path)
	}
	// The endpoint is not paginated and rejects `limit` with
	// parameter_unknown, so we must not send one.
	if raw := last.URL.RawQuery; strings.Contains(raw, "limit") {
		t.Errorf("capabilities must not send limit, got query %q", raw)
	}
}

func TestPersons_RelationshipFilters(t *testing.T) {
	body := `{"object":"list","data":[{"id":"person_1","object":"person"}],"has_more":false,"url":"/v1/accounts/acct_1/persons"}`

	t.Run("no filter omits the relationship hash", func(t *testing.T) {
		srv, last := recordingServer(t, body)
		testutil.CaptureStdout(t)
		if err := runPersons(context.Background(), testutil.NewOpts(srv.URL), []string{"acct_1"}); err != nil {
			t.Fatalf("runPersons: %v", err)
		}
		if raw := last.URL.RawQuery; strings.Contains(raw, "relationship") {
			t.Errorf("expected no relationship params, got %q", raw)
		}
	})

	t.Run("owner and director pass through", func(t *testing.T) {
		srv, last := recordingServer(t, body)
		testutil.CaptureStdout(t)
		args := []string{"acct_1", "--relationship-owner", "--relationship-director"}
		if err := runPersons(context.Background(), testutil.NewOpts(srv.URL), args); err != nil {
			t.Fatalf("runPersons: %v", err)
		}
		q := last.URL.Query()
		if q.Get("relationship[owner]") != "true" {
			t.Errorf("relationship[owner] = %q", q.Get("relationship[owner]"))
		}
		if q.Get("relationship[director]") != "true" {
			t.Errorf("relationship[director] = %q", q.Get("relationship[director]"))
		}
	})
}

// external-accounts has no dedicated endpoint: it must retrieve the account
// with an explicit expand and project the nested list out.
func TestExternalAccounts_ExpandsAndProjects(t *testing.T) {
	body := `{"id":"acct_1","object":"account","external_accounts":{"object":"list","has_more":false,` +
		`"data":[{"id":"ba_1","object":"bank_account","last4":"6789"}]}}`
	srv, last := recordingServer(t, body)
	testutil.CaptureStdout(t)
	if err := runExternalAccounts(context.Background(), testutil.NewOpts(srv.URL), []string{"acct_1"}); err != nil {
		t.Fatalf("runExternalAccounts: %v", err)
	}
	if last.URL.Path != "/v1/accounts/acct_1" {
		t.Errorf("path = %q", last.URL.Path)
	}
	if !strings.Contains(last.URL.RawQuery, "external_accounts") {
		t.Errorf("expected expand[]=external_accounts, got query %q", last.URL.RawQuery)
	}
}

// The wrapper tags BankAccount/Card `json:"-"`, so marshalling it directly
// loses everything but {id, object}. Regression guard: the concrete object's
// fields must survive.
func TestTypedExternalAccountItems_UnwrapsConcreteTypes(t *testing.T) {
	list := &stripeapi.AccountExternalAccountList{
		Data: []*stripeapi.AccountExternalAccount{
			{ID: "ba_1", Type: stripeapi.AccountExternalAccountTypeBankAccount,
				BankAccount: &stripeapi.BankAccount{ID: "ba_1", BankName: "Test Bank", Last4: "6789"}},
			{ID: "card_1", Type: stripeapi.AccountExternalAccountTypeCard,
				Card: &stripeapi.Card{ID: "card_1", Brand: stripeapi.CardBrandVisa, Last4: "4242"}},
		},
	}
	items, err := typedExternalAccountItems(list)
	if err != nil {
		t.Fatalf("externalAccountItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0]["bank_name"] != "Test Bank" || items[0]["last4"] != "6789" {
		t.Errorf("bank account fields lost in round-trip: %v", items[0])
	}
	if items[1]["brand"] != "Visa" || items[1]["last4"] != "4242" {
		t.Errorf("card fields lost in round-trip: %v", items[1])
	}
}

func TestTypedExternalAccountItems_EmptyIsNotAnError(t *testing.T) {
	// An account with nothing attached is a valid answer, not an error.
	items, err := typedExternalAccountItems(nil)
	if err != nil || len(items) != 0 {
		t.Errorf("expected empty result for nil list, got %v / %v", items, err)
	}
}

// External accounts are the one place --raw cannot use the SDK's per-object
// recorded body: they are parsed out of the account payload, so they have no
// response of their own. Sourcing them from the parent's body is what keeps
// --raw honest here — a silent fall back to the struct path would return
// exactly the truncated {id, object} answer this subcommand exists to avoid.
func TestExternalAccounts_RawSourcesFromTheParentBody(t *testing.T) {
	const body = `{"id":"acct_1","object":"account","external_accounts":{"object":"list","has_more":false,` +
		`"data":[{"id":"ba_1","object":"bank_account","last4":"6789","bank_name":"Test Bank",` +
		`"future_field_the_sdk_cannot_model":"kept"}]}}`
	srv, _ := recordingServer(t, body)

	out := testutil.WithCapturedStdout(t, func() {
		if err := runExternalAccounts(context.Background(), testutil.NewRawOpts(srv.URL), []string{"acct_1"}); err != nil {
			t.Errorf("runExternalAccounts: %v", err)
		}
	})

	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("decode envelope: %v\noutput: %s", err, out)
	}
	items, ok := env["data"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one external account, got %s", out)
	}
	ea, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item is not an object: %v", items[0])
	}
	if ea["last4"] != "6789" || ea["bank_name"] != "Test Bank" {
		t.Errorf("raw projection lost the destination details: %v", ea)
	}
	if ea["future_field_the_sdk_cannot_model"] != "kept" {
		t.Errorf("raw projection fell back to the struct path: %v", ea)
	}
}

// No external_accounts on the body is the same answer as a nil typed list:
// nowhere to pay out to yet, not a failure.
func TestExternalAccounts_RawWithNoListIsNotAnError(t *testing.T) {
	srv, _ := recordingServer(t, `{"id":"acct_1","object":"account"}`)
	testutil.CaptureStdout(t)
	if err := runExternalAccounts(context.Background(), testutil.NewRawOpts(srv.URL), []string{"acct_1"}); err != nil {
		t.Errorf("runExternalAccounts: %v", err)
	}
}

func TestRequiredPositionals(t *testing.T) {
	opts := testutil.NewOpts("http://127.0.0.1:1")
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"get", runGet(context.Background(), opts, nil)},
		{"capabilities", runCapabilities(context.Background(), opts, nil)},
		{"persons", runPersons(context.Background(), opts, nil)},
		{"external-accounts", runExternalAccounts(context.Background(), opts, nil)},
	} {
		if tc.err == nil {
			t.Errorf("%s: expected a usage error with no positional arg", tc.name)
		} else if !strings.Contains(tc.err.Error(), "usage:") {
			t.Errorf("%s: expected usage error, got %v", tc.name, tc.err)
		}
	}
}

func TestRun_UnknownSubcommand(t *testing.T) {
	err := Run(context.Background(), testutil.NewOpts("http://127.0.0.1:1"), []string{"capabilties"})
	if err == nil {
		t.Fatal("expected an error for an unknown subcommand")
	}
	if !strings.Contains(err.Error(), "capabilties") {
		t.Errorf("error should name the bad subcommand, got %v", err)
	}
}

// End-to-end guard for the omitempty loss: a false charges_enabled must
// survive ToRawMap *and* the output package's empty-pruning walk, all the way
// to the emitted envelope. Asserting on ToRawMap alone would miss a later
// regression in the renderer.
func TestGet_FalseBooleansReachTheEnvelope(t *testing.T) {
	body := `{"id":"acct_broken","object":"account","country":"GB",` +
		`"charges_enabled":false,"payouts_enabled":false,"details_submitted":false,` +
		`"requirements":{"disabled_reason":"requirements.past_due"}}`
	srv, _ := recordingServer(t, body)

	out := captureEnvelopeJSON(t, func() error {
		return runGet(context.Background(), testutil.NewOpts(srv.URL), []string{"acct_broken"})
	})
	data, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("data not an object: %T", out["data"])
	}
	for _, k := range []string{"charges_enabled", "payouts_enabled", "details_submitted"} {
		v, present := data[k]
		if !present {
			t.Errorf("%s missing from emitted envelope — a broken account renders as an unknown one", k)
			continue
		}
		if v != false {
			t.Errorf("%s = %v, want false", k, v)
		}
	}
}

// captureEnvelopeJSON runs fn with stdout piped and decodes the emitted line.
func captureEnvelopeJSON(t *testing.T, fn func() error) map[string]any {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan []byte, 1)
	go func() { b, _ := io.ReadAll(r); done <- b }()
	runErr := fn()
	_ = w.Close()
	os.Stdout = old
	body := <-done
	if runErr != nil {
		t.Fatalf("run: %v (out=%q)", runErr, body)
	}
	var env map[string]any
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope: %v (out=%q)", err, body)
	}
	return env
}

// The list path marshals through a different helper than get. When those were
// two separate JSON round-trips the omitempty fix landed on get only, and
// `list` — the command you would use to survey a whole portfolio for broken
// accounts — kept dropping the field.
func TestList_FalseBooleansReachTheEnvelope(t *testing.T) {
	body := `{"object":"list","has_more":false,"url":"/v1/accounts","data":[` +
		`{"id":"acct_broken","object":"account","charges_enabled":false,"payouts_enabled":false},` +
		`{"id":"acct_ok","object":"account","charges_enabled":true,"payouts_enabled":true}]}`
	srv, _ := recordingServer(t, body)

	out := captureEnvelopeJSON(t, func() error {
		return runList(context.Background(), testutil.NewOpts(srv.URL), nil)
	})
	rows, ok := out["data"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %v", out["data"])
	}
	broken := rows[0].(map[string]any)
	if v, present := broken["charges_enabled"]; !present || v != false {
		t.Errorf("broken account charges_enabled: present=%v value=%v, want present=true value=false", present, v)
	}
	healthy := rows[1].(map[string]any)
	if healthy["charges_enabled"] != true {
		t.Errorf("healthy account charges_enabled = %v, want true", healthy["charges_enabled"])
	}
}

// The header is set unconditionally at the transport, so a platform-scoped
// command must refuse the flag rather than answer from the wrong books.
func TestRun_RejectsStripeAccount(t *testing.T) {
	opts := testutil.NewOptsForAccount("http://127.0.0.1:1", "acct_x")
	for _, sub := range []string{"list", "get", "capabilities", "persons", "external-accounts"} {
		err := Run(context.Background(), opts, []string{sub, "acct_1"})
		if err == nil {
			t.Errorf("%s: expected --stripe-account to be rejected", sub)
			continue
		}
		var oe *output.Error
		if !errors.As(err, &oe) {
			t.Errorf("%s: want *output.Error, got %T", sub, err)
			continue
		}
		if oe.By != output.FixableByAgent {
			t.Errorf("%s: fixableBy = %q, want agent", sub, oe.By)
		}
	}
	// help stays reachable regardless — it makes no request.
	if err := Run(context.Background(), opts, []string{"usage"}); err != nil {
		t.Errorf("usage should not be gated: %v", err)
	}
}
