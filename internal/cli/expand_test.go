package cli

import (
	"reflect"
	"testing"
)

func TestSplitExpandRoutesDottedToPaths(t *testing.T) {
	tests := []struct {
		in         string
		wantLeaves []string
		wantPaths  []string
	}{
		{"", nil, nil},
		{"description", []string{"description"}, nil},
		{"lines.data.description", nil, []string{"lines.data.description"}},
		{"description,lines.data.description", []string{"description"}, []string{"lines.data.description"}},
		{" foo , a.b , bar ", []string{"foo", "bar"}, []string{"a.b"}},
	}
	for _, tc := range tests {
		leaves, paths := splitExpand(tc.in)
		if !reflect.DeepEqual(leaves, tc.wantLeaves) {
			t.Errorf("splitExpand(%q) leaves = %#v, want %#v", tc.in, leaves, tc.wantLeaves)
		}
		if !reflect.DeepEqual(paths, tc.wantPaths) {
			t.Errorf("splitExpand(%q) paths = %#v, want %#v", tc.in, paths, tc.wantPaths)
		}
	}
}

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
