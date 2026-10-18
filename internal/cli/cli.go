// Package cli parses arguments without reading host state.
package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kydex/abx/internal/profile"
)

type Kind string

const (
	Run           Kind = "run"
	Shell         Kind = "shell"
	Work          Kind = "work"
	Inspect       Kind = "inspect"
	Verify        Kind = "verify"
	Help          Kind = "help"
	Version       Kind = "version"
	ProfileCreate Kind = "profile create"
	ProfileShow   Kind = "profile show"
	ProfileList   Kind = "profile list"
)

type Command struct {
	Kind    Kind
	Profile string
	Args    []string
}

const Usage = `abx:
  abx profile create <profile>
  abx profile show <profile>
  abx profile list
  abx run <profile> [-- <argument>...]
  abx shell <profile>
  abx work <profile>
  abx inspect <profile>
  abx verify <profile>
  abx help | -h | --help
  abx version | -v | --version
No arguments: same as "abx help"; a bare -- adds no run arguments.
Storage: $XDG_DATA_HOME/abx (used only when absolute), or .local/share/abx in the passwd home.`

func Parse(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{Kind: Help}, nil
	}
	k := Kind(args[0])
	// Help and version are recognised only where the command itself goes.
	// Scanning later positions made "profile create demo --version" print the
	// version, exit 0 and create nothing: a silent no-op that reads as success.
	// Subcommands keep their documented "no extra arguments" refusal instead.
	switch k {
	case "-h", "--help":
		k = Help
	case "-v", "--version":
		k = Version
	}
	switch k {
	case "profile":
		if len(args) < 2 {
			return Command{}, errors.New("profile requires create, show or list")
		}
		k = Kind("profile " + args[1])
		switch k {
		case ProfileList:
			if len(args) != 2 {
				return Command{}, errors.New("profile list takes no arguments")
			}
			return Command{Kind: k}, nil
		case ProfileCreate, ProfileShow:
			if len(args) != 3 {
				return Command{}, errors.New(string(k) + " requires exactly one name")
			}
			if strings.HasPrefix(args[2], "-") {
				return Command{}, unknownOption(args[2], k)
			}
			if err := profile.ValidateName(args[2]); err != nil {
				return Command{}, err
			}
			return Command{Kind: k, Profile: args[2]}, nil
		default:
			return Command{}, fmt.Errorf("unknown profile command %q", args[1])
		}
	case Help, Version:
		if len(args) != 1 {
			return Command{}, fmt.Errorf("%s takes no arguments", args[0])
		}
		return Command{Kind: k}, nil
	case Run, Shell, Work, Inspect, Verify:
		if len(args) < 2 {
			return Command{}, fmt.Errorf("%s requires a profile", k)
		}
		// A leading hyphen is a mistyped flag, not a name: say so and point
		// at the help, instead of reciting the profile name rules.
		if strings.HasPrefix(args[1], "-") {
			return Command{}, unknownOption(args[1], k)
		}
		if err := profile.ValidateName(args[1]); err != nil {
			return Command{}, err
		}
		c := Command{Kind: k, Profile: args[1]}
		if len(args) > 2 {
			if k == Run && args[2] != "--" {
				return Command{}, fmt.Errorf("run arguments must follow --: " +
					"abx run <profile> -- <argument> [additional arguments]")
			}
			if k != Run {
				return Command{}, fmt.Errorf("unexpected arguments to %s", k)
			}
			c.Args = slices.Clone(args[3:])
		}
		return c, nil
	default:
		return Command{}, fmt.Errorf("unknown command %q", args[0])
	}
}

// unknownOption explains a leading-hyphen token in a name position as the
// mistyped flag it is, pointing at the help instead of the name rules.
func unknownOption(arg string, k Kind) error {
	return fmt.Errorf("unknown option %q for %s; run \"abx --help\"", arg, k)
}
