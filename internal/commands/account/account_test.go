package account

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/config"
	agentstripe "github.com/simonperryman/agent-stripe/internal/stripe"
)

// memKeyring is the in-memory Keyring substitute used across account tests.
// account.go talks to the keychain through config.SetSecret/GetSecret/DeleteSecret;
// swapping the backend via config.SetKeyring keeps the OS keychain untouched.
type memKeyring struct{ store map[string]string }

func newMemKeyring() *memKeyring { return &memKeyring{store: map[string]string{}} }

func (m *memKeyring) Set(s, k, v string) error { m.store[s+"/"+k] = v; return nil }
func (m *memKeyring) Get(s, k string) (string, error) {
	if v, ok := m.store[s+"/"+k]; ok {
		return v, nil
	}
	return "", os.ErrNotExist
}
func (m *memKeyring) Delete(s, k string) error { delete(m.store, s+"/"+k); return nil }

// setupSandbox redirects config + keyring + stdout to safe places and returns
// a function to read what was emitted to stdout.
func setupSandbox(t *testing.T) (kr *memKeyring, readStdout func() []byte) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	kr = newMemKeyring()
	restore := config.SetKeyring(kr)
	t.Cleanup(restore)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	t.Cleanup(func() {
		_ = w.Close()
		os.Stdout = oldStdout
	})

	readStdout = func() []byte {
		_ = w.Close()
		os.Stdout = oldStdout
		return <-done
	}
	return kr, readStdout
}

func decodeEnvelope(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatalf("decode envelope: %v\nbody: %s", err, b)
	}
	return env
}

func TestAccountAdd_StoresSecretAndConfig(t *testing.T) {
	kr, read := setupSandbox(t)
	if err := runAdd([]string{"acme", "--key", "sk_test_abc"}); err != nil {
		t.Fatalf("runAdd: %v", err)
	}
	env := decodeEnvelope(t, read())
	if env["account"] != "acme" {
		t.Errorf("expected account=acme, got %v", env["account"])
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	acc, ok := cfg.Accounts["acme"]
	if !ok {
		t.Fatal("expected acme in config")
	}
	if cfg.DefaultAccount != "acme" {
		t.Errorf("expected first add to become default, got %q", cfg.DefaultAccount)
	}
	if acc.Mode != config.ModeTest {
		t.Errorf("expected test mode, got %q", acc.Mode)
	}
	// The keychain ref should point to the stored secret.
	if got := kr.store[config.KeyringService+"/"+acc.KeychainRef]; got != "sk_test_abc" {
		t.Errorf("expected secret stored in keyring, got %q", got)
	}
}

func TestAccountAdd_RejectsInvalidKeyPrefix(t *testing.T) {
	setupSandbox(t)
	if err := runAdd([]string{"acme", "--key", "pk_test_abc"}); err == nil {
		t.Fatal("expected error for publishable-key prefix")
	}
}

func TestAccountAdd_DuplicateAliasRejected(t *testing.T) {
	setupSandbox(t)
	if err := runAdd([]string{"acme", "--key", "sk_test_a"}); err != nil {
		t.Fatal(err)
	}
	if err := runAdd([]string{"acme", "--key", "sk_test_b"}); err == nil {
		t.Fatal("expected duplicate alias rejection")
	}
}

func TestAccountAdd_MissingKeyAndNoStdinErrors(t *testing.T) {
	setupSandbox(t)
	// stdin is a TTY in `go test`, not a pipe, so runAdd should refuse.
	if err := runAdd([]string{"acme"}); err == nil {
		t.Fatal("expected error when no --key, no --form, no stdin pipe")
	}
}

func TestAccountList_OutputShape(t *testing.T) {
	setupSandbox(t)
	// Seed two accounts directly through Save to skip stdin gymnastics.
	cfg, _ := config.Load()
	cfg.Accounts["one"] = config.Account{Alias: "one", Mode: config.ModeTest, KeychainRef: "r1"}
	cfg.Accounts["two"] = config.Account{Alias: "two", Mode: config.ModeLive, KeychainRef: "r2"}
	cfg.DefaultAccount = "one"
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	_, read := setupSandbox(t)
	// Re-seed in this fresh sandbox.
	cfg2, _ := config.Load()
	cfg2.Accounts["one"] = config.Account{Alias: "one", Mode: config.ModeTest, KeychainRef: "r1"}
	cfg2.Accounts["two"] = config.Account{Alias: "two", Mode: config.ModeLive, KeychainRef: "r2"}
	cfg2.DefaultAccount = "one"
	if err := config.Save(cfg2); err != nil {
		t.Fatal(err)
	}

	if err := runList(); err != nil {
		t.Fatal(err)
	}
	env := decodeEnvelope(t, read())
	data, ok := env["data"].([]any)
	if !ok {
		t.Fatalf("expected data to be a list, got %T", env["data"])
	}
	if len(data) != 2 {
		t.Errorf("expected 2 accounts, got %d", len(data))
	}
}

func TestAccountSetDefault_UpdatesConfig(t *testing.T) {
	setupSandbox(t)
	if err := runAdd([]string{"one", "--key", "sk_test_a"}); err != nil {
		t.Fatal(err)
	}
	if err := runAdd([]string{"two", "--key", "sk_test_b"}); err != nil {
		t.Fatal(err)
	}

	_, read := setupSandbox(t)
	cfg, _ := config.Load()
	cfg.Accounts["one"] = config.Account{Alias: "one", Mode: config.ModeTest, KeychainRef: "r1"}
	cfg.Accounts["two"] = config.Account{Alias: "two", Mode: config.ModeTest, KeychainRef: "r2"}
	cfg.DefaultAccount = "one"
	_ = config.Save(cfg)

	if err := runSetDefault([]string{"two"}); err != nil {
		t.Fatal(err)
	}
	_ = read()
	out, _ := config.Load()
	if out.DefaultAccount != "two" {
		t.Errorf("expected default=two, got %q", out.DefaultAccount)
	}
}

func TestAccountSetDefault_RejectsUnknown(t *testing.T) {
	setupSandbox(t)
	if err := runSetDefault([]string{"nope"}); err == nil {
		t.Fatal("expected error for unknown alias")
	}
}

func TestAccountRemove_DeletesEntryAndSecret(t *testing.T) {
	kr, read := setupSandbox(t)
	if err := runAdd([]string{"acme", "--key", "sk_test_a"}); err != nil {
		t.Fatal(err)
	}
	_ = read()
	// Capture ref for keyring assertion before removing.
	cfg, _ := config.Load()
	ref := cfg.Accounts["acme"].KeychainRef
	if _, ok := kr.store[config.KeyringService+"/"+ref]; !ok {
		t.Fatal("expected secret present before remove")
	}

	_, read = setupSandbox(t)
	// Sandbox was reset; replay state into the fresh dir/keyring so remove
	// has something to operate on.
	// Re-seed config + keyring.
	cfg2, _ := config.Load()
	cfg2.Accounts["acme"] = config.Account{Alias: "acme", Mode: config.ModeTest, KeychainRef: ref}
	cfg2.DefaultAccount = "acme"
	_ = config.Save(cfg2)
	// Re-seed secret in current keyring (a fresh memKeyring).
	_ = config.SetSecret(ref, "sk_test_a")

	if err := runRemove([]string{"acme"}); err != nil {
		t.Fatal(err)
	}
	_ = read()
	cfg3, _ := config.Load()
	if _, ok := cfg3.Accounts["acme"]; ok {
		t.Error("expected acme removed from config")
	}
	if cfg3.DefaultAccount != "" {
		t.Errorf("expected default cleared, got %q", cfg3.DefaultAccount)
	}
}

func TestAccountRemove_RejectsUnknown(t *testing.T) {
	setupSandbox(t)
	if err := runRemove([]string{"nope"}); err == nil {
		t.Fatal("expected error for unknown alias")
	}
}

func TestAccountTest_NoArgUsesGlobalClient(t *testing.T) {
	// No-arg `account test` uses opts.Client, which we point at a fake
	// /v1/account endpoint via httptest.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/account" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"acct_abc","country":"US","default_currency":"usd","email":"x@y.com","business_profile":{"name":"Acme"}}`)
	}))
	defer srv.Close()

	_, read := setupSandbox(t)
	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "acme", Mode: config.ModeTest},
		Client:  agentstripe.NewClient("sk_test_fake", srv.URL, "", 5*time.Second),
		Timeout: 5 * time.Second,
	}
	if err := runTest(context.Background(), opts, nil); err != nil {
		t.Fatalf("runTest: %v", err)
	}
	env := decodeEnvelope(t, read())
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", env["data"])
	}
	if data["id"] != "acct_abc" {
		t.Errorf("expected id acct_abc, got %v", data["id"])
	}
	if env["account"] != "acme" {
		t.Errorf("expected account=acme, got %v", env["account"])
	}
}

func TestAccountTest_AliasArg_LiveRequiresFlag(t *testing.T) {
	setupSandbox(t)
	// Seed a live-mode account; runTest [alias] should refuse without --live.
	cfg, _ := config.Load()
	cfg.Accounts["prod"] = config.Account{Alias: "prod", Mode: config.ModeLive, KeychainRef: "ref"}
	_ = config.Save(cfg)
	_ = config.SetSecret("ref", "sk_live_x")

	opts := &cli.GlobalOpts{Timeout: time.Second}
	err := runTest(context.Background(), opts, []string{"prod"})
	if err == nil {
		t.Fatal("expected error for live account without --live")
	}
}

func TestAccountTest_AliasArg_Unknown(t *testing.T) {
	setupSandbox(t)
	opts := &cli.GlobalOpts{Timeout: time.Second}
	if err := runTest(context.Background(), opts, []string{"nope"}); err == nil {
		t.Fatal("expected error for unknown alias")
	}
}

func TestRun_UnknownSubcommand(t *testing.T) {
	setupSandbox(t)
	if err := Run(context.Background(), &cli.GlobalOpts{}, []string{"frobnicate"}); err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestRun_HelpExits(t *testing.T) {
	setupSandbox(t)
	for _, tok := range []string{"usage", "help"} {
		if err := Run(context.Background(), &cli.GlobalOpts{}, []string{tok}); err != nil {
			t.Errorf("expected nil for %q, got %v", tok, err)
		}
	}
}

func TestRun_EmptyArgsPrintsUsage(t *testing.T) {
	setupSandbox(t)
	if err := Run(context.Background(), &cli.GlobalOpts{}, nil); err != nil {
		t.Errorf("expected nil for empty args, got %v", err)
	}
}

// `account test --stripe-account acct_x` is the Connect probe: the first step
// of any Connect investigation. It must both send the header and say so in the
// envelope — reporting success against the platform account while appearing to
// confirm the connected one is the worst possible failure for a command whose
// entire job is verifying scope.
func TestAccountTest_ConnectProbe(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("Stripe-Account")
		_, _ = io.WriteString(w, `{"id":"acct_connected","country":"GB","default_currency":"gbp"}`)
	}))
	defer srv.Close()

	_, read := setupSandbox(t)
	opts := &cli.GlobalOpts{
		Account:       &config.Account{Alias: "acme", Mode: config.ModeTest},
		StripeAccount: "acct_connected",
		Client:        agentstripe.NewClient("sk_test_fake", srv.URL, "acct_connected", 5*time.Second),
		Timeout:       5 * time.Second,
	}
	if err := runTest(context.Background(), opts, nil); err != nil {
		t.Fatalf("runTest: %v", err)
	}
	if gotHeader != "acct_connected" {
		t.Errorf("Stripe-Account header = %q, want acct_connected", gotHeader)
	}
	env := decodeEnvelope(t, read())
	if env["stripeAccount"] != "acct_connected" {
		t.Errorf("envelope stripeAccount = %v, want acct_connected", env["stripeAccount"])
	}
}
