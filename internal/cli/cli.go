// Package cli parses arguments without reading host state.
package cli

import (
	"fmt"

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
  abx profile create <name>
  abx profile show <name>
  abx profile list
  abx run <profile> [-- <argument>...]
  abx shell <profile>
  abx work <profile>
  abx inspect <profile>
  abx verify <profile>
  abx help | -h | --help
  abx version
Storage: $XDG_DATA_HOME/abx, or .local/share/abx in the passwd home.`

func Parse(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("command required")
	}
	k := Kind(args[0])
	if k == "-h" || k == "--help" {
		k = Help
	}
	switch k {
	case "profile":
		if len(args) < 2 {
			return Command{}, fmt.Errorf("profile requires create, show or list")
		}
		k = Kind("profile " + args[1])
		switch k {
		case ProfileList:
			if len(args) != 2 {
				return Command{}, fmt.Errorf("profile list takes no arguments")
			}
			return Command{Kind: k}, nil
		case ProfileCreate, ProfileShow:
			if len(args) != 3 {
				return Command{}, fmt.Errorf("%s requires exactly one name", k)
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
			return Command{}, fmt.Errorf("%s takes no arguments", k)
		}
		return Command{Kind: k}, nil
	case Run, Shell, Work, Inspect, Verify:
		if len(args) < 2 {
			return Command{}, fmt.Errorf("%s requires a profile", k)
		}
		if err := profile.ValidateName(args[1]); err != nil {
			return Command{}, err
		}
		c := Command{Kind: k, Profile: args[1]}
		if len(args) > 2 {
			if k != Run || args[2] != "--" {
				return Command{}, fmt.Errorf("unexpected arguments to %s", k)
			}
			c.Args = append([]string(nil), args[3:]...)
		}
		return c, nil
	default:
		return Command{}, fmt.Errorf("unknown command %q", args[0])
	}
}
