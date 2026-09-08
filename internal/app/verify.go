package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/kydex/abx/internal/bwrap"
	"github.com/kydex/abx/internal/host"
	"github.com/kydex/abx/internal/probe"
	"github.com/kydex/abx/internal/sandbox"
)

func runVerify(ctx context.Context, in *host.Inputs, home string, env []string, stdio bwrap.Stdio, openTool func(context.Context) (*bwrap.Tool, error)) (status int, result error) {
	input, err := sessionInput(in, env)
	if err != nil {
		return 1, err
	}
	input.Command = []string{sandbox.ProbePath, "__probe", "{}"}
	if _, err = sandbox.BuildVerification(input); err != nil {
		return 1, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tool, err := openTool(ctx)
	if err != nil {
		return 1, err
	}
	defer finishClose(&status, &result, tool.Close)
	// /proc/self/exe denotes this running binary, even after its installed path is replaced.
	self, err := os.Open("/proc/self/exe")
	if err != nil {
		return 1, err
	}
	defer finishClose(&status, &result, self.Close)
	expected, err := observeHost(home, input)
	if err != nil {
		return 1, err
	}
	files := &probeFiles{}
	defer finishClose(&status, &result, files.Close)
	expected.Sentinel, expected.Link, err = files.create(home, in.Project())
	if err != nil {
		return 1, err
	}
	raw, err := json.Marshal(expected)
	if err != nil {
		return 1, err
	}
	input.Command[2] = string(raw)
	plan, err := sandbox.BuildVerification(input)
	if err != nil {
		return 1, err
	}
	if err = in.Prepare(); err != nil {
		return 1, err
	}
	if err = in.Revalidate(); err != nil {
		return 1, err
	}
	status, err = bwrap.Execute(ctx, tool, plan, func(path string) (*os.File, error) {
		if path == "/proc/self/exe" {
			return self, nil
		}
		return in.SourceFile(path)
	}, stdio)
	if err != nil {
		return 1, err
	}
	if status != 0 {
		return 1, fmt.Errorf("verification process exited with status %d", status)
	}
	// Final success is printed only after outer resource and temporary-file cleanup.
	return 0, nil
}
func observeHost(home string, input sandbox.Input) (probe.Expected, error) {
	e := probe.Expected{Home: home, Objects: map[string]probe.Identity{}, Skills: input.Skills != ""}
	for _, p := range []string{home, "/tmp", "/var/tmp", "/run", "/dev"} {
		id, err := probe.Identify(p)
		if err != nil {
			return e, err
		}
		e.Objects[p] = id
	}
	var err error
	e.Profile, err = probe.Identify(input.Home)
	if err != nil {
		return e, err
	}
	e.Project, err = probe.Identify(input.Project)
	if err != nil {
		return e, err
	}
	e.Namespaces = make(map[string]string)
	for _, name := range probe.NamespaceNames() {
		e.Namespaces[name], err = os.Readlink("/proc/self/ns/" + name)
		if err != nil {
			return e, fmt.Errorf("observe host %s namespace: %w", name, err)
		}
	}
	for _, v := range input.Environment {
		e.Environment = append(e.Environment, v.Name+"="+v.Value)
	}
	for _, m := range input.System {
		if m.Kind == sandbox.ReadOnly {
			e.ReadOnly = append(e.ReadOnly, m.Target)
		}
	}
	if e.Skills {
		e.ReadOnly = append(e.ReadOnly, "/home/agent/.agents/skills")
	}
	e.ReadOnly = append(e.ReadOnly, sandbox.ProbePath)
	return e, nil
}

type probeFile struct {
	path string
	info os.FileInfo
}
type probeFiles struct{ files []probeFile }

func (p *probeFiles) create(home, project string) (sentinel, link string, result error) {
	f, err := os.CreateTemp(home, ".abx-verify-*")
	if err != nil {
		return "", "", err
	}
	info, err := f.Stat()
	if err != nil {
		return "", "", errors.Join(err, f.Close(), os.Remove(f.Name()))
	}
	p.files = append(p.files, probeFile{f.Name(), info})
	if err = f.Close(); err != nil {
		return "", "", err
	}
	target := filepath.Join(project, filepath.Base(f.Name()))
	if err = os.Symlink(f.Name(), target); err != nil {
		return "", "", err
	}
	info, err = os.Lstat(target)
	if err != nil {
		return "", "", errors.Join(err, os.Remove(target))
	}
	p.files = append(p.files, probeFile{target, info})
	return f.Name(), filepath.Join("/workspace", filepath.Base(target)), nil
}
func (p *probeFiles) Close() error {
	var result error
	for i := len(p.files) - 1; i >= 0; i-- {
		f := p.files[i]
		info, err := os.Lstat(f.path)
		if err == nil && !os.SameFile(info, f.info) {
			err = errors.New("temporary probe object was replaced")
		}
		if err == nil {
			err = os.Remove(f.path)
		}
		if err != nil {
			result = errors.Join(result, fmt.Errorf("cleanup %q: %w", f.path, err))
		}
	}
	p.files = nil
	return result
}

// Keep success reporting after runSession's descriptor and probe cleanup.
func verifySuccess(out io.Writer) error {
	_, err := io.WriteString(out, "Verification passed; temporary probe files removed.\n")
	return err
}
