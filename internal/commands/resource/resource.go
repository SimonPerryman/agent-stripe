// Package resource implements `agent-stripe resource describe <name>`. It is
// a *meta* command — it does not call Stripe; it reflects over the stripe-go
// SDK types so an agent can learn a resource's field shape and recommended
// --expand-stripe paths without spending an API call.
package resource

import (
	"context"
	"fmt"
	"os"

	"github.com/simonperryman/agent-stripe/internal/cli"
)

const Usage = `resource — discover Stripe resource shapes (no API call)

Subcommands:
  describe <name> [--depth N]     Emit a field tree + recommended expandPaths
                                  for a resource. Default depth 3.

Resources: customer, charge, payment-intent, refund, dispute, balance,
           payout, event, subscription, invoice, product, price.

This command does NOT hit Stripe. The field tree is reflected from stripe-go's
struct definitions, and expandPaths is a hand-curated list mirroring the
recommendations each resource's "usage" mentions. Use this when you want to
know "what can I expand?" or "what fields exist on a subscription?" before
spending an API call.`

func Run(ctx context.Context, opts *cli.GlobalOpts, args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	switch args[0] {
	case "describe":
		return runDescribe(ctx, opts, args[1:])
	case "usage", "help":
		fmt.Fprintln(os.Stderr, Usage)
		return nil
	}
	return fmt.Errorf("unknown resource subcommand %q", args[0])
}
