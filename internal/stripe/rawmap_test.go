package stripe

import (
	"testing"

	stripeapi "github.com/stripe/stripe-go/v85"
)

// stripe-go tags charges_enabled / payouts_enabled / details_submitted
// `omitempty`, so marshalling the struct deletes them when false — i.e. in
// exactly the case someone is trying to diagnose. Without the restore, a
// broken account and an account Stripe never reported on are indistinguishable.
func TestAccountFalseBooleansSurviveMarshalling(t *testing.T) {
	acct := &stripeapi.Account{
		ID: "acct_1", Object: "account", Country: "GB",
		ChargesEnabled: false, PayoutsEnabled: false, DetailsSubmitted: false,
	}
	m, err := ToRawMap(acct, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"charges_enabled", "payouts_enabled", "details_submitted"} {
		v, ok := m[k]
		if !ok {
			t.Fatalf("%s missing — omitempty ate the false case", k)
		}
		if v != false {
			t.Fatalf("%s = %v, want false", k, v)
		}
	}
}

func TestAccountTrueBooleansStillMarshal(t *testing.T) {
	acct := &stripeapi.Account{
		ID: "acct_1", Object: "account",
		ChargesEnabled: true, PayoutsEnabled: true, DetailsSubmitted: true,
	}
	m, err := ToRawMap(acct, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"charges_enabled", "payouts_enabled", "details_submitted"} {
		if m[k] != true {
			t.Errorf("%s = %v, want true", k, m[k])
		}
	}
}

// An unexpanded `acct_…` reference parses into an otherwise-zero struct with
// no object field. Stamping charges_enabled:false on that would invent a
// signal rather than restore one.
func TestUnexpandedAccountReferenceIsNotStamped(t *testing.T) {
	m, err := ToRawMap(&stripeapi.Account{ID: "acct_1"}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"charges_enabled", "payouts_enabled", "details_submitted"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s should be absent on an id-only reference, got %v", k, m[k])
		}
	}
}

// Other resources' response booleans carry no `omitempty`, so the restore
// list stays minimal. If a future SDK bump adds one, this documents the
// assumption that made that safe.
func TestOtherResourcesDoNotNeedRestoring(t *testing.T) {
	m, err := ToRawMap(&stripeapi.Charge{ID: "ch_1", Object: "charge", Captured: false, Refunded: false}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"captured", "refunded"} {
		if _, ok := m[k]; !ok {
			t.Errorf("charge.%s went missing — it may have gained omitempty; add it to restoreOmitemptyBools", k)
		}
	}
}
