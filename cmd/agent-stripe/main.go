// agent-stripe is a read-only Stripe CLI for AI agents. See README.md.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/commands/account"
	"github.com/simonperryman/agent-stripe/internal/commands/applicationfee"
	"github.com/simonperryman/agent-stripe/internal/commands/balance"
	"github.com/simonperryman/agent-stripe/internal/commands/charge"
	"github.com/simonperryman/agent-stripe/internal/commands/checkoutsession"
	"github.com/simonperryman/agent-stripe/internal/commands/connectedaccount"
	"github.com/simonperryman/agent-stripe/internal/commands/customer"
	"github.com/simonperryman/agent-stripe/internal/commands/dispute"
	"github.com/simonperryman/agent-stripe/internal/commands/event"
	"github.com/simonperryman/agent-stripe/internal/commands/invoice"
	"github.com/simonperryman/agent-stripe/internal/commands/invoiceitem"
	"github.com/simonperryman/agent-stripe/internal/commands/paymentintent"
	"github.com/simonperryman/agent-stripe/internal/commands/paymentmethod"
	"github.com/simonperryman/agent-stripe/internal/commands/payout"
	"github.com/simonperryman/agent-stripe/internal/commands/price"
	"github.com/simonperryman/agent-stripe/internal/commands/product"
	"github.com/simonperryman/agent-stripe/internal/commands/refund"
	"github.com/simonperryman/agent-stripe/internal/commands/resource"
	"github.com/simonperryman/agent-stripe/internal/commands/setupintent"
	"github.com/simonperryman/agent-stripe/internal/commands/subscription"
	"github.com/simonperryman/agent-stripe/internal/commands/subscriptionitem"
	"github.com/simonperryman/agent-stripe/internal/commands/subscriptionschedule"
	"github.com/simonperryman/agent-stripe/internal/commands/transfer"
	"github.com/simonperryman/agent-stripe/internal/commands/webhookendpoint"
)

func main() {
	ctx, cancel := signalContext()
	defer cancel()

	cli.Dispatch(ctx, buildRegistry(), os.Args[1:])
}

// buildRegistry returns the registry of top-level commands wired into the
// binary. Extracted from main so tests can assert the expected commands are
// registered without invoking cli.Dispatch (which exits the process).
func buildRegistry() *cli.Registry {
	return &cli.Registry{
		Commands: map[string]cli.CommandSpec{
			"account":               {Run: account.Run, Usage: account.Usage},
			"connected-account":     {Run: connectedaccount.Run, Usage: connectedaccount.Usage},
			"application-fee":       {Run: applicationfee.Run, Usage: applicationfee.Usage},
			"customer":              {Run: customer.Run, Usage: customer.Usage},
			"event":                 {Run: event.Run, Usage: event.Usage},
			"charge":                {Run: charge.Run, Usage: charge.Usage},
			"payment-intent":        {Run: paymentintent.Run, Usage: paymentintent.Usage},
			"payment-method":        {Run: paymentmethod.Run, Usage: paymentmethod.Usage},
			"setup-intent":          {Run: setupintent.Run, Usage: setupintent.Usage},
			"refund":                {Run: refund.Run, Usage: refund.Usage},
			"dispute":               {Run: dispute.Run, Usage: dispute.Usage},
			"balance":               {Run: balance.Run, Usage: balance.Usage},
			"payout":                {Run: payout.Run, Usage: payout.Usage},
			"transfer":              {Run: transfer.Run, Usage: transfer.Usage},
			"checkout-session":      {Run: checkoutsession.Run, Usage: checkoutsession.Usage},
			"subscription":          {Run: subscription.Run, Usage: subscription.Usage},
			"subscription-item":     {Run: subscriptionitem.Run, Usage: subscriptionitem.Usage},
			"subscription-schedule": {Run: subscriptionschedule.Run, Usage: subscriptionschedule.Usage},
			"invoice":               {Run: invoice.Run, Usage: invoice.Usage},
			"invoice-item":          {Run: invoiceitem.Run, Usage: invoiceitem.Usage},
			"product":               {Run: product.Run, Usage: product.Usage},
			"price":                 {Run: price.Run, Usage: price.Usage},
			"webhook-endpoint":      {Run: webhookendpoint.Run, Usage: webhookendpoint.Usage},
			"resource":              {Run: resource.Run, Usage: resource.Usage, NoAccount: true}, // pure reflection, no Stripe call
		},
	}
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
