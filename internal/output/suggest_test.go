package output

import "testing"

func TestClosest(t *testing.T) {
	candidates := []string{"charge", "customer", "refund", "payment-intent"}
	cases := []struct {
		name, in, want string
	}{
		{"exact miss returns empty", "totallydifferent", ""},
		{"one-char typo matches", "chage", "charge"},
		{"case insensitive", "REFUND", "refund"},
		{"empty input returns empty", "", ""},
		{"within len/3 threshold", "paymen-intent", "payment-intent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Closest(tc.in, candidates); got != tc.want {
				t.Errorf("Closest(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
	if got := Closest("charge", nil); got != "" {
		t.Errorf("empty candidates: got %q", got)
	}
}

func TestValidList(t *testing.T) {
	if got := ValidList(nil); got != "" {
		t.Errorf("ValidList(nil) = %q, want empty", got)
	}
	if got := ValidList([]string{"a", "b", "c"}); got != "valid: a, b, c" {
		t.Errorf("ValidList = %q", got)
	}
}
