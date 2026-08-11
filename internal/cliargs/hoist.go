// Package cliargs normalizes the raw process argv before urfave/cli sees it.
package cliargs

import "strings"

// globalStringFlags are the app-level flags that take a value. Their value
// may arrive as either "--api-url X" (two tokens) or "--api-url=X" (one).
var globalStringFlags = map[string]bool{
	"--output":          true,
	"-o":                true,
	"--api-url":         true,
	"--api-key":         true,
	"--sandbox-api-url": true,
	"--sandbox-gateway": true,
}

// globalBoolFlags are the app-level flags that take no value.
var globalBoolFlags = map[string]bool{
	"--debug": true,
	"-d":      true,
}

// Hoist moves app-level flags in front of the subcommand so that
// "createos sandbox create --output json" behaves like
// "createos --output json sandbox create".
//
// urfave/cli v2 only parses app-level flags before the first subcommand
// token; anything after it is handed to the subcommand's own flag set and
// fails with "flag provided but not defined". Rewriting argv is the one
// change that fixes every command at once — mirroring hidden flags onto each
// command would let them parse but leave their values unread, because the
// App.Before hook that consumes them runs before subcommand parsing.
//
// Everything after a bare "--" is left untouched: those tokens belong to the
// user's own command line (e.g. "sandbox exec box -- ./ci.sh --debug").
func Hoist(args []string) []string {
	if len(args) < 2 {
		return args
	}

	hoisted := []string{}
	rest := []string{}

	for i := 1; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			rest = append(rest, args[i:]...)
			break
		}

		name, _, hasInlineValue := strings.Cut(arg, "=")

		switch {
		case globalBoolFlags[arg]:
			hoisted = append(hoisted, arg)
		case globalStringFlags[name] && hasInlineValue:
			hoisted = append(hoisted, arg)
		case globalStringFlags[arg]:
			// Value is the next token; take it along, if present.
			hoisted = append(hoisted, arg)
			if i+1 < len(args) {
				i++
				hoisted = append(hoisted, args[i])
			}
		default:
			rest = append(rest, arg)
		}
	}

	if len(hoisted) == 0 {
		return args
	}

	out := make([]string, 0, len(args))
	out = append(out, args[0])
	out = append(out, hoisted...)
	out = append(out, rest...)
	return out
}
