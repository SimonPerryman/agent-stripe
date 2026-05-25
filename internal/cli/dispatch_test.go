package cli

import (
	"flag"
	"strings"
	"testing"
	"time"
)

// newGlobalsFlagSet mirrors the FlagSet registered in Dispatch. Kept in sync
// with dispatch.go so tests can exercise global-flag parsing without invoking
// Dispatch (which calls os.Exit).
func newGlobalsFlagSet() (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("agent-stripe", flag.ContinueOnError)
	fs.SetOutput(new(strings.Builder)) // swallow usage on error
	account := fs.String("account", "", "account alias")
	fs.Bool("live", false, "")
	fs.Bool("full", false, "")
	fs.String("expand", "", "")
	fs.String("expand-stripe", "", "")
	fs.Bool("stream", false, "")
	fs.Float64("rate-limit", 15.0, "")
	fs.Duration("timeout", 30*time.Second, "")
	return fs, account
}

func TestGlobalsFlagSet_AccountLongForm(t *testing.T) {
	fs, account := newGlobalsFlagSet()
	if err := fs.Parse(ReorderFlagsFirst([]string{"--account", "prod", "charge", "list"}, fs)); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if *account != "prod" {
		t.Fatalf("--account: got %q, want %q", *account, "prod")
	}
	if rest := fs.Args(); len(rest) != 2 || rest[0] != "charge" || rest[1] != "list" {
		t.Fatalf("positional args: got %v, want [charge list]", rest)
	}
}

func TestGlobalsFlagSet_ShortAliasRejected(t *testing.T) {
	fs, _ := newGlobalsFlagSet()
	err := fs.Parse(ReorderFlagsFirst([]string{"-a", "prod", "charge", "list"}, fs))
	if err == nil {
		t.Fatal("expected -a to be rejected as unknown flag, got nil error")
	}
}

func TestIsHelpToken(t *testing.T) {
	want := []string{"usage", "help", "-h", "--help"}
	for _, tok := range want {
		if !isHelpToken(tok) {
			t.Errorf("isHelpToken(%q) = false, want true", tok)
		}
	}
	for _, tok := range []string{"", "list", "get", "-help", "--h", "uSage"} {
		if isHelpToken(tok) {
			t.Errorf("isHelpToken(%q) = true, want false", tok)
		}
	}
}

// needsAccount must return false for every help token so per-command help is
// reachable without credentials. This is the regression guard for the bug
// where `agent-stripe charge usage` failed with "no account specified".
func TestNeedsAccount_HelpTokensBypassAccount(t *testing.T) {
	spec := CommandSpec{Usage: "x"}
	for _, tok := range []string{"usage", "help", "-h", "--help"} {
		if needsAccount(spec, "charge", []string{tok}) {
			t.Errorf("needsAccount(charge, [%q]) = true, want false", tok)
		}
	}
}

func TestNeedsAccount_RealSubcommandsStillRequireAccount(t *testing.T) {
	spec := CommandSpec{Usage: "x"}
	for _, sub := range []string{"list", "get", "search"} {
		if !needsAccount(spec, "charge", []string{sub}) {
			t.Errorf("needsAccount(charge, [%q]) = false, want true", sub)
		}
	}
}
