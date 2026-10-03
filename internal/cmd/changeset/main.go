package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || (args[0] != "version" && args[0] != "lint") {
		fmt.Fprintln(stderr, "usage: changeset version|lint")
		return 2
	}
	frags, err := collect(".changes")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	latestTag := latest()
	lv, _ := parseVersion(latestTag)
	v, err := next(lv, frags)
	if len(frags) > 0 && err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if args[0] == "lint" {
		fmt.Fprintf(stdout, "%d changeset(s) valid\n", len(frags))
		return 0
	}
	if len(frags) == 0 {
		fmt.Fprintln(stderr, "no pending changesets")
		return 1
	}
	old, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	out := render(string(old), entry(v, time.Now().UTC(), frags), v, latestTag)
	if err := os.WriteFile("CHANGELOG.md", []byte(out), 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, f := range frags {
		if err := os.Remove(f.file); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	fmt.Fprintln(stdout, v)
	return 0
}

// latest returns the newest v-prefixed root tag, or v0.0.0 when there is none.
func latest() string {
	out, err := exec.Command("git", "describe", "--tags", "--abbrev=0", "--match", "v[0-9]*").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "v0.0.0"
	}
	return strings.TrimSpace(string(out))
}
