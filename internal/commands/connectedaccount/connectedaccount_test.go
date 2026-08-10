package connectedaccount

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
func TestExternalAccountItems_UnwrapsConcreteTypes(t *testing.T) {
	list := &stripeapi.AccountExternalAccountList{
		Data: []*stripeapi.AccountExternalAccount{
			{ID: "ba_1", Type: stripeapi.AccountExternalAccountTypeBankAccount,
				BankAccount: &stripeapi.BankAccount{ID: "ba_1", BankName: "Test Bank", Last4: "6789"}},
			{ID: "card_1", Type: stripeapi.AccountExternalAccountTypeCard,
				Card: &stripeapi.Card{ID: "card_1", Brand: stripeapi.CardBrandVisa, Last4: "4242"}},
		},
	}
	items, err := externalAccountItems(list)
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

func TestExternalAccountItems_EmptyIsNotAnError(t *testing.T) {
	// An account with nothing attached is a valid answer, not an error.
	items, err := externalAccountItems(nil)
	if err != nil || len(items) != 0 {
		t.Errorf("expected empty result for nil list, got %v / %v", items, err)
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
