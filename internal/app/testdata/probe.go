// This is a live-test fixture, not a public command or a product verification implementation.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

type Expect struct {
	Namespace, Home, Sentinel string
	Alternatives              []string
	Objects                   map[string][2]uint64
}

func die(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(91) }
func identity(path string) [2]uint64 {
	info, e := os.Stat(path)
	if e != nil {
		die("stat " + path)
	}
	s := info.Sys().(*syscall.Stat_t)
	return [2]uint64{uint64(s.Dev), s.Ino}
}
func main() {
	if len(os.Args) != 2 {
		die("mode missing")
	}
	if os.Args[1] == "userns-child" {
		return
	}
	child := exec.Command("/proc/self/exe", "userns-child")
	child.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWUSER}
	if err := child.Run(); !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.ENOSPC) {
		die(fmt.Sprintf("nested user namespace was not denied as expected: %v", err))
	}
	mode := os.Args[1]
	if mode != "run" && mode != "shell" && mode != "work" {
		die("invalid mode")
	}
	var expected Expect
	data, e := os.ReadFile("/home/agent/expect.json")
	if e != nil {
		die("expectations missing")
	}
	if json.Unmarshal(data, &expected) != nil {
		die("invalid expectations")
	}
	cwd, e := os.Getwd()
	if e != nil {
		die("cwd")
	}
	want := "/workspace"
	if mode == "shell" {
		want = "/home/agent"
		if _, e = os.Stat("/workspace"); !os.IsNotExist(e) {
			die("shell exposed workspace")
		}
	}
	if cwd != want || os.Getenv("HOME") != "/home/agent" || os.Getenv("PWD") != want {
		die("home/cwd mismatch")
	}
	for _, key := range []string{"ABX_TEST_HOST_LEAK", "SSH_AUTH_SOCK", "NODE_OPTIONS", "HTTP_PROXY"} {
		if _, ok := os.LookupEnv(key); ok {
			die("host environment leak: " + key)
		}
	}
	for _, p := range []string{expected.Sentinel, "/etc/shadow"} {
		if _, e = os.Stat(p); !os.IsNotExist(e) {
			die("external data visible: " + p)
		}
	}
	if _, e = os.Stat(expected.Home); e == nil {
		if identity(expected.Home) == expected.Objects[expected.Home] {
			die("real home visible")
		}
	} else if !os.IsNotExist(e) {
		die("cannot determine home visibility")
	}
	ns, e := os.Readlink("/proc/self/ns/pid")
	if e != nil || ns == expected.Namespace {
		die("PID namespace not isolated")
	}
	for _, p := range []string{"/tmp", "/var/tmp", "/run", "/dev"} {
		if identity(p) == expected.Objects[p] {
			die("host object exposed: " + p)
		}
	}
	for _, p := range []string{"/tmp", "/var/tmp", "/run"} {
		var s syscall.Statfs_t
		if syscall.Statfs(p, &s) != nil || s.Type != 0x01021994 {
			die("not tmpfs: " + p)
		}
	}
	info, e := os.Stat("/run/user")
	if e != nil || info.Mode().Perm() != 0700 {
		die("runtime mode")
	}
	mounts, e := os.ReadFile("/proc/self/mountinfo")
	if e != nil {
		die("mountinfo")
	}
	targets := []string{"/usr", "/home/agent/.agents/skills"}
	if len(expected.Alternatives) > 0 {
		targets = append(targets, "/etc/alternatives")
	}
	for _, path := range expected.Alternatives {
		if _, err := os.Stat(path); err != nil {
			die("broken alternatives chain: " + path)
		}
	}
	for _, target := range targets {
		found := false
		for _, line := range strings.Split(string(mounts), "\n") {
			f := strings.Fields(line)
			if len(f) > 5 && f[4] == target {
				if !strings.Contains(","+f[5]+",", ",ro,") {
					die("writable mount: " + target)
				}
				found = true
			}
		}
		if !found {
			die("missing mount: " + target)
		}
	}
	if marker, e := os.ReadFile("/home/agent/.agents/skills/marker"); e != nil || string(marker) != "shared" {
		die("skills unavailable")
	}
	if target, e := os.Readlink("/etc/ssl/cert.pem"); e == nil {
		_ = target
		if _, e = os.ReadFile("/etc/ssl/cert.pem"); e != nil {
			die("TLS certificate link broken")
		}
	}
	for _, dir := range []string{"/home/agent", want} {
		p := filepath.Join(dir, "probe-write")
		if e = os.WriteFile(p, []byte("ok"), 0600); e != nil {
			die("not writable: " + dir)
		}
		if os.Remove(p) != nil {
			die("cleanup")
		}
	}
	fmt.Println("probe:" + mode)
}
