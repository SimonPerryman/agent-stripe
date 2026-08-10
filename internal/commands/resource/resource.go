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
	"github.com/simonperryman/agent-stripe/internal/output"
)

const Usage = `resource — discover Stripe resource shapes (no API call)

Subcommands:
  describe <name> [--depth N]     Emit a field tree + recommended expandPaths
                                  for a resource. Default depth 3.

Resources: customer, charge, payment-intent, refund, dispute, balance,
           payout, event, subscription, invoice, product, price.

This command does NOT hit Stripe — safe to run without --account or a configured
account. The field tree is reflected from stripe-go's struct definitions, and
the expandPaths output field is a hand-curated, machine-readable mirror of
each resource's --expand-stripe recommendation. Use this when you want to
know "what can I expand?" or "what fields exist on a subscription?" before
spending an API call.

Scope: describe can only ever show ONE version's shape — the one this CLI is
built against, reported as apiVersion in the envelope. It reflects over the
SDK's structs, so --api-version is rejected here rather than answered with a
tree that does not represent it, and --raw has nothing to apply to. Those
structs are also narrower than the wire: a field Stripe sends that this SDK
version does not model will not appear below. To see what an object actually
carries, fetch a real one with --raw.

Help: usage | help | -h | --help`

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
	return &output.Error{
		Msg:  fmt.Sprintf("unknown resource subcommand %q", args[0]),
		Hint: cli.SubcommandHint(args[0], []string{"describe"}),
		By:   output.FixableByAgent,
	}
}
