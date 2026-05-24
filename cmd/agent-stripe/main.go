// agent-stripe is a read-only Stripe CLI for AI agents. See README.md.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/shhac/agent-stripe/internal/cli"
	"github.com/shhac/agent-stripe/internal/commands/account"
	"github.com/shhac/agent-stripe/internal/commands/balance"
	"github.com/shhac/agent-stripe/internal/commands/charge"
	"github.com/shhac/agent-stripe/internal/commands/customer"
	"github.com/shhac/agent-stripe/internal/commands/dispute"
	"github.com/shhac/agent-stripe/internal/commands/event"
	"github.com/shhac/agent-stripe/internal/commands/paymentintent"
	"github.com/shhac/agent-stripe/internal/commands/payout"
	"github.com/shhac/agent-stripe/internal/commands/refund"
)

func main() {
	ctx, cancel := signalContext()
	defer cancel()

	reg := &cli.Registry{
		Commands: map[string]cli.CommandRunner{
			"account":        account.Run,
			"customer":       customer.Run,
			"event":          event.Run,
			"charge":         charge.Run,
			"payment-intent": paymentintent.Run,
			"refund":         refund.Run,
			"dispute":        dispute.Run,
			"balance":        balance.Run,
			"payout":         payout.Run,
		},
		UsageStrings: map[string]string{
			"account":        account.Usage,
			"customer":       customer.Usage,
			"event":          event.Usage,
			"charge":         charge.Usage,
			"payment-intent": paymentintent.Usage,
			"refund":         refund.Usage,
			"dispute":        dispute.Usage,
			"balance":        balance.Usage,
			"payout":         payout.Usage,
		},
	}
	cli.Dispatch(ctx, reg, os.Args[1:])
}

// signalContext cancels on SIGINT/SIGTERM and emits an `{"error":"interrupted"}`
// envelope to stderr before the process tears down — so an agent reading stderr
// sees a structured reason for the non-zero exit.
func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"error": "interrupted", "fixableBy": "retry"})
		cancel()
		// Give in-flight work a moment to unwind before forcing exit.
		fmt.Fprintln(os.Stderr) // flush
		os.Exit(130)
	}()
	return ctx, cancel
}
