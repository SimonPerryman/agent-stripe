package config

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	in := &Config{
		DefaultAccount: "acme",
		Accounts: map[string]Account{
			"acme": {Alias: "acme", Mode: ModeTest, KeychainRef: "uuid-1"},
		},
	}
	if err := saveTo(p, in); err != nil {
		t.Fatal(err)
	}
	out, err := loadFrom(p)
	if err != nil {
		t.Fatal(err)
	}
	if out.DefaultAccount != "acme" || out.Accounts["acme"].KeychainRef != "uuid-1" {
		t.Fatalf("round trip mismatch: %+v", out)
	}
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "nope.json")
	out, err := loadFrom(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Accounts) != 0 {
		t.Fatalf("expected empty accounts")
	}
}

func TestDeriveMode(t *testing.T) {
	cases := []struct {
		key  string
		want Mode
		err  bool
	}{
		{"sk_test_abc", ModeTest, false},
		{"sk_live_abc", ModeLive, false},
		{"rk_test_abc", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := DeriveMode(c.key)
		if (err != nil) != c.err {
			t.Fatalf("DeriveMode(%q): err=%v want err=%v", c.key, err, c.err)
		}
		if got != c.want {
			t.Fatalf("DeriveMode(%q): got %q want %q", c.key, got, c.want)
		}
	}
}
