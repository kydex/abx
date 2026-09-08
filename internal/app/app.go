// Package app owns command order, resource lifetime and diagnostics.
package app

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kydex/abx/internal/bwrap"
	"github.com/kydex/abx/internal/cli"
	"github.com/kydex/abx/internal/host"
	"github.com/kydex/abx/internal/probe"
	"github.com/kydex/abx/internal/sandbox"
)

//go:embed VERSION
var versionText string

// Version is loaded from VERSION, the single release-version source.
var Version = strings.TrimSpace(versionText)

func Run(ctx context.Context, args []string, stdio bwrap.Stdio) int {
	fail := func(code int, err error) int {
		_, _ = fmt.Fprintf(stdio.Err, "abx: %s\n", Escape(err.Error()))
		return code
	}
	if os.Getuid() == 0 {
		return fail(1, errors.New("must not run as root"))
	}
	if len(args) > 0 && args[0] == "__probe" {
		if len(args) != 2 {
			return fail(2, errors.New("invalid private probe invocation"))
		}
		if err := probe.Run(args[1], stdio.Out); err != nil {
			return fail(1, err)
		}
		return 0
	}
	c, err := cli.Parse(args)
	if err != nil {
		return fail(2, err)
	}
	switch c.Kind {
	case cli.Help:
		_, err = fmt.Fprintln(stdio.Out, cli.Usage)
	case cli.Version:
		_, err = fmt.Fprintf(stdio.Out, "abx %s\n", Escape(Version))
	default:
		status, e := runSession(ctx, c, os.Environ(), stdio)
		if e != nil {
			return fail(1, e)
		}
		if c.Kind == cli.Verify && status == 0 {
			if err := verifySuccess(stdio.Out); err != nil {
				return fail(1, err)
			}
		}
		return status
	}
	if err != nil {
		return fail(1, err)
	}
	return 0
}
func value(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimPrefix(env[i], prefix)
		}
	}
	return ""
}
func runSession(ctx context.Context, c cli.Command, env []string, stdio bwrap.Stdio) (int, error) {
	account, err := host.ResolveAccount()
	if err != nil {
		return 1, err
	}
	return runSessionForAccount(ctx, c, env, stdio, account)
}

// runSessionForAccount runs one session after the invoking account has been resolved.
func runSessionForAccount(ctx context.Context, c cli.Command, env []string, stdio bwrap.Stdio, account host.Account) (status int, result error) {
	paths, err := host.ResolvePaths(account, value(env, "XDG_DATA_HOME"))
	if err != nil {
		return 1, err
	}
	switch c.Kind {
	case cli.ProfileCreate, cli.ProfileShow, cli.ProfileList:
		return runProfiles(c, account, paths, stdio.Out)
	}
	mode := host.Run
	switch c.Kind {
	case cli.Shell:
		mode = host.Shell
	case cli.Work:
		mode = host.Work
	}
	cwd := ""
	if c.Kind != cli.Shell {
		cwd, err = os.Getwd()
		if err != nil {
			return 1, err
		}
	}
	var in *host.Inputs
	if c.Kind == cli.Verify {
		in, err = host.ResolveVerification(account, paths, c.Profile, cwd)
	} else {
		in, err = host.Resolve(account, paths, c.Profile, cwd, mode, c.Args)
	}
	if err != nil {
		return 1, err
	}
	defer finishClose(&status, &result, in.Close)
	if c.Kind == cli.Verify {
		return runVerify(ctx, in, account.Home, env, stdio, bwrap.Open)
	}
	plan, err := buildPlan(in, env)
	if err != nil {
		return 1, err
	}
	if c.Kind == cli.Inspect {
		_, err = io.WriteString(stdio.Out, formatPlan(plan))
		return 0, err
	}
	return executeSession(ctx, in, plan, stdio, bwrap.Open, bwrap.Execute)
}

// The private runtime seams test ordering around host revalidation without launching Bubblewrap.
func executeSession(
	ctx context.Context,
	in *host.Inputs,
	plan sandbox.Plan,
	stdio bwrap.Stdio,
	openTool func(context.Context) (*bwrap.Tool, error),
	execute func(context.Context, *bwrap.Tool, sandbox.Plan, func(string) (*os.File, error), bwrap.Stdio) (int, error),
) (status int, result error) {
	tool, err := openTool(ctx)
	if err != nil {
		return 1, err
	}
	defer finishClose(&status, &result, tool.Close)
	if err = in.Prepare(); err != nil {
		return 1, err
	}
	if err = in.Revalidate(); err != nil {
		return 1, err
	}
	return execute(ctx, tool, plan, in.SourceFile, stdio)
}

func finishClose(status *int, result *error, close func() error) {
	if err := close(); err != nil {
		*result = errors.Join(*result, err)
		*status = 1
	}
}

func buildPlan(in *host.Inputs, env []string) (sandbox.Plan, error) {
	input, err := sessionInput(in, env)
	if err != nil {
		return sandbox.Plan{}, err
	}
	return sandbox.Build(input)
}

func sessionInput(in *host.Inputs, env []string) (sandbox.Input, error) {
	cwd := "/workspace"
	var mode sandbox.Mode
	switch in.Mode() {
	case host.Run:
		mode = sandbox.Run
	case host.Shell:
		mode = sandbox.Shell
		cwd = "/home/agent"
	case host.Work:
		mode = sandbox.Work
	default:
		return sandbox.Input{}, errors.New("invalid host session mode")
	}
	clean, err := sandbox.Session(in.Username(), in.Shell(), cwd, env)
	if err != nil {
		return sandbox.Input{}, err
	}
	var mounts []sandbox.Mount
	for _, s := range in.System() {
		kind := sandbox.ReadOnly
		source := s.Path
		if s.Link != "" {
			kind = sandbox.Symlink
			source = s.Link
		}
		mounts = append(mounts, sandbox.Mount{Kind: kind, Source: source, Target: s.Target})
	}
	return sandbox.Input{Home: in.Home(), Project: in.Project(), Skills: in.Skills(), Mode: mode, System: mounts, Environment: clean, Command: in.Command()}, nil
}
