package cli

import (
	"flag"
	"io"
	"reflect"
	"testing"
)

func newAccountAddFS() *flag.FlagSet {
	fs := flag.NewFlagSet("account add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.String("key", "", "")
	fs.Bool("form", false, "")
	fs.Bool("default", false, "")
	return fs
}

func TestReorderFlagsFirst(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			"bool flag after positional",
			[]string{"it-smoke", "--default"},
			[]string{"--default", "it-smoke"},
		},
		{
			"value flag consumes next token, not positional",
			[]string{"it-smoke", "--key", "sk_test_X"},
			[]string{"--key", "sk_test_X", "it-smoke"},
		},
		{
			"flag=value form stays one token",
			[]string{"it-smoke", "--key=sk_test_X"},
			[]string{"--key=sk_test_X", "it-smoke"},
		},
		{
			"mixed positional and multiple flags",
			[]string{"it-smoke", "--key", "sk_test_X", "--default"},
			[]string{"--key", "sk_test_X", "--default", "it-smoke"},
		},
		{
			"-- terminator freezes everything after",
			[]string{"it-smoke", "--", "--default", "-x"},
			[]string{"it-smoke", "--", "--default", "-x"},
		},
		{
			"unknown flag left in place (fs.Parse will reject)",
			[]string{"it-smoke", "--bogus", "--default"},
			[]string{"--default", "it-smoke", "--bogus"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ReorderFlagsFirst(tc.in, newAccountAddFS())
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestReorderThenParseAccountAdd(t *testing.T) {
	fs := newAccountAddFS()
	reordered := ReorderFlagsFirst([]string{"it-smoke", "--key", "sk_test_X", "--default"}, fs)
	if err := fs.Parse(reordered); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := fs.Lookup("key").Value.String(); got != "sk_test_X" {
		t.Fatalf("key = %q", got)
	}
	if got := fs.Lookup("default").Value.String(); got != "true" {
		t.Fatalf("default = %q", got)
	}
	if rest := fs.Args(); len(rest) != 1 || rest[0] != "it-smoke" {
		t.Fatalf("positional = %#v", rest)
	}
}

func TestReorderPreservesUnknownSubcommandFlag(t *testing.T) {
	// Mimics the dispatch case: global FS knows --stream and --full, doesn't
	// know --limit (a subcommand flag). The reorder pulls --stream to the
	// front and leaves --limit + its value alongside the subcommand args.
	fs := flag.NewFlagSet("global", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Bool("stream", false, "")
	fs.Bool("full", false, "")
	in := []string{"charge", "list", "--limit", "5", "--stream"}
	want := []string{"--stream", "charge", "list", "--limit", "5"}
	if got := ReorderFlagsFirst(in, fs); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
