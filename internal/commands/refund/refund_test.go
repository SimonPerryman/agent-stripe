package refund

import (
	"context"
	"strings"
	"testing"

	"github.com/simonperryman/agent-stripe/internal/cli"
	"github.com/simonperryman/agent-stripe/internal/config"
)

func TestRefundList_RejectsBothChargeAndPaymentIntent(t *testing.T) {
	opts := &cli.GlobalOpts{
		Account: &config.Account{Alias: "test", Mode: config.ModeTest},
	}
	err := runList(context.Background(), opts, []string{"--charge", "ch_1", "--payment-intent", "pi_1"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutual-exclusion error, got %v", err)
	}
}
