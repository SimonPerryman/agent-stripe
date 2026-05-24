package cli

import (
	"flag"
	"strings"
)

// ReorderFlagsFirst returns args with all recognized flag tokens moved to the
// front and positionals to the back, so a subsequent fs.Parse accepts
// interleaved flags + positionals (`account add it-smoke --key X --default`).
//
// Unknown flags are left in their original position — under
// flag.ContinueOnError that lets fs.Parse error on them as before, and avoids
// swallowing a positional value behind a misspelled flag. A `--` token ends
// reordering: it and everything after stays positional.
//
// Bool flags are detected via the stdlib `interface{ IsBoolFlag() bool }`
// contract, so `--default` doesn't consume the next token but `--key X` does.
func ReorderFlagsFirst(args []string, fs *flag.FlagSet) []string {
	var flags, rest []string
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			rest = append(rest, a)
			i++
			continue
		}
		name := a
		hasInlineValue := false
		if eq := strings.Index(a, "="); eq >= 0 {
			name = a[:eq]
			hasInlineValue = true
		}
		name = strings.TrimLeft(name, "-")
		f := fs.Lookup(name)
		if f == nil {
			rest = append(rest, a)
			i++
			continue
		}
		flags = append(flags, a)
		if hasInlineValue {
			i++
			continue
		}
		isBool := false
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			isBool = true
		}
		if !isBool && i+1 < len(args) {
			flags = append(flags, args[i+1])
			i += 2
			continue
		}
		i++
	}
	return append(flags, rest...)
}
