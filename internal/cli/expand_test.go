package cli

import (
	"reflect"
	"testing"
)

func TestSplitCSV_ExpandStripe(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"customer", []string{"customer"}},
		{"customer,latest_charge", []string{"customer", "latest_charge"}},
		{"  customer , latest_charge  ", []string{"customer", "latest_charge"}},
		{"a,b,c", []string{"a", "b", "c"}},
	}
	for _, tc := range tests {
		got := splitCSV(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitCSV(%q) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
}
