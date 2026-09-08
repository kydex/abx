package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/kydex/abx/internal/cli"
	"github.com/kydex/abx/internal/host"
)

func runProfiles(c cli.Command, account host.Account, paths host.Paths, out io.Writer) (int, error) {
	var text strings.Builder
	switch c.Kind {
	case cli.ProfileCreate:
		home, err := host.CreateProfile(account, paths, c.Profile)
		if err != nil {
			return 1, err
		}
		fmt.Fprintf(&text, "Created profile %s\nHome: %s\n", Escape(c.Profile), Escape(home))
	case cli.ProfileShow:
		status, err := host.ShowProfile(account, paths, c.Profile)
		if err != nil {
			return 1, err
		}
		fmt.Fprintf(&text, "Profile: %s\nHome: %s\n", Escape(status.Name), Escape(status.Home))
		if !status.Exists {
			text.WriteString("State: absent\n")
			break
		}
		text.WriteString("State: present\n")
		if status.AgentError != nil {
			fmt.Fprintf(&text, "Command: unavailable (%s)\n", Escape(status.AgentError.Error()))
		} else {
			fmt.Fprintf(&text, "Command: %s\n", Escape(status.Executable))
		}
		fmt.Fprintf(&text, "Skills: %s\n", status.Skills)
		if status.SkillsError != nil {
			fmt.Fprintf(&text, "Skills detail: %s\n", Escape(status.SkillsError.Error()))
		}
	case cli.ProfileList:
		statuses, err := host.ListProfiles(account, paths)
		if err != nil {
			return 1, err
		}
		if len(statuses) == 0 {
			break
		}
		text.WriteString("PROFILE  COMMAND  SKILLS\n")
		for _, status := range statuses {
			command := "available"
			if status.AgentError != nil {
				command = "unavailable"
			}
			fmt.Fprintf(&text, "%s  %s  %s\n", Escape(status.Name), command, status.Skills)
		}
	default:
		return 1, fmt.Errorf("unsupported profile operation %q", c.Kind)
	}
	_, err := io.WriteString(out, text.String())
	if err != nil {
		if c.Kind == cli.ProfileCreate {
			err = fmt.Errorf("profile was created, but its confirmation could not be written: %w", err)
		}
		return 1, err
	}
	return 0, nil
}
