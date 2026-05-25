package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Every registered command must expose `usage`, `help`, `-h`, and `--help`
// without requiring a configured Stripe account. An agent encountering the
// tool for the first time uses these to discover the surface area before it
// has any credentials in the keychain — gating help behind account config
// is the bug this test guards against.
func TestPerCommandHelpReachableWithoutAccount(t *testing.T) {
	bin := buildBinary(t)

	// `resource` already opts out of account resolution; the others all
	// require one for real subcommands, so they're the ones that prove the
	// help bypass works.
	commands := []string{
		"account", "balance", "charge", "customer", "dispute", "event",
		"invoice", "payment-intent", "payout", "price", "product",
		"refund", "resource", "subscription", "transfer",
	}
	helpTokens := []string{"usage", "help", "-h", "--help"}

	for _, cmd := range commands {
		for _, tok := range helpTokens {
			name := cmd + " " + tok
			t.Run(name, func(t *testing.T) {
				// Point HOME at an empty temp dir so any config lookup
				// finds nothing — proves we never reach account resolution.
				c := exec.Command(bin, cmd, tok)
				c.Env = append([]string{}, "HOME="+t.TempDir(), "PATH=/usr/bin:/bin")
				var stdout, stderr bytes.Buffer
				c.Stdout = &stdout
				c.Stderr = &stderr
				err := c.Run()
				if err != nil {
					t.Fatalf("exit error: %v\nstderr: %s", err, stderr.String())
				}
				// Help is printed to stderr by design (so stdout stays
				// reserved for structured output). Confirm we got the
				// command's Usage block, not an error envelope.
				out := stderr.String()
				if strings.Contains(out, `"error"`) {
					t.Fatalf("got error envelope instead of help:\n%s", out)
				}
				if !strings.Contains(out, cmd) {
					t.Errorf("help output for %q does not mention command name:\n%s", cmd, out)
				}
			})
		}
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "agent-stripe")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	// `.` here refers to cmd/agent-stripe — the package this test lives in.
	cmd := exec.Command("go", "build", "-o", bin, ".")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build: %v\n%s", err, stderr.String())
	}
	return bin
}
