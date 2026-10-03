// Package main is the release changeset tool: it validates .changes/*.md fragments and
// aggregates them into CHANGELOG.md. It uses only the standard library and is not importable.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const repoURL = "https://github.com/madebyclowd/go-auto-sequence"

var (
	typeOrder    = []string{"Added", "Changed", "Fixed", "Removed", "Deprecated", "Security"}
	bumpPriority = map[string]int{"patch": 1, "minor": 2, "major": 3}
	versionRe    = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)
	placeholder  = "Nothing has been released yet."
)

// version is a plain semantic version without pre-release or build parts.
type version struct{ major, minor, patch int }

func parseVersion(s string) (version, error) {
	m := versionRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return version{}, fmt.Errorf("invalid version %q", s)
	}
	n := func(i int) int { v, _ := strconv.Atoi(m[i]); return v }
	return version{n(1), n(2), n(3)}, nil
}

func (v version) String() string { return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch) }

func (v version) less(o version) bool {
	if v.major != o.major {
		return v.major < o.major
	}
	if v.minor != o.minor {
		return v.minor < o.minor
	}
	return v.patch < o.patch
}

// bump applies a changeset level. Below 1.0.0 a major bump raises the minor number and
// anything else raises the patch number; the tool never crosses to 1.0.0 on its own
// (use release-as).
func (v version) bump(level string) version {
	if v.major == 0 {
		if level == "major" {
			return version{0, v.minor + 1, 0}
		}
		return version{0, v.minor, v.patch + 1}
	}
	switch level {
	case "major":
		return version{v.major + 1, 0, 0}
	case "minor":
		return version{v.major, v.minor + 1, 0}
	}
	return version{v.major, v.minor, v.patch + 1}
}

// fragment is one parsed .changes/*.md file.
type fragment struct {
	file      string
	bump      string
	typ       string
	releaseAs string
	body      string
}

func parseFragment(name, contents string) (fragment, error) {
	contents = strings.ReplaceAll(contents, "\r\n", "\n")
	if !strings.HasPrefix(contents, "---\n") {
		return fragment{}, fmt.Errorf("%s: missing frontmatter", name)
	}
	rest := contents[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return fragment{}, fmt.Errorf("%s: unterminated frontmatter", name)
	}
	f := fragment{file: name, body: strings.TrimSpace(strings.TrimPrefix(rest[end+4:], "\n"))}
	for _, line := range strings.Split(rest[:end], "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			return fragment{}, fmt.Errorf("%s: malformed frontmatter line %q", name, line)
		}
		switch k = strings.TrimSpace(k); k {
		case "bump":
			f.bump = strings.TrimSpace(v)
		case "type":
			f.typ = strings.TrimSpace(v)
		case "release-as":
			f.releaseAs = strings.TrimSpace(v)
		default:
			return fragment{}, fmt.Errorf("%s: unknown frontmatter key %q", name, k)
		}
	}
	if _, ok := bumpPriority[f.bump]; !ok {
		return fragment{}, fmt.Errorf("%s: bump must be patch, minor or major, got %q", name, f.bump)
	}
	if !contains(typeOrder, f.typ) {
		return fragment{}, fmt.Errorf("%s: type must be one of %s, got %q", name, strings.Join(typeOrder, ", "), f.typ)
	}
	if f.body == "" {
		return fragment{}, fmt.Errorf("%s: empty body", name)
	}
	if f.releaseAs != "" {
		if _, err := parseVersion(f.releaseAs); err != nil {
			return fragment{}, fmt.Errorf("%s: release-as: %w", name, err)
		}
	}
	return f, nil
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// collect reads every fragment in dir, skipping README.md and TEMPLATE.md. Any invalid
// fragment fails the whole run (the PHP script skipped them silently).
func collect(dir string) ([]fragment, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var out []fragment
	var errs []error
	for _, p := range files {
		base := filepath.Base(p)
		if base == "README.md" || base == "TEMPLATE.md" {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		f, err := parseFragment(base, string(b))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		f.file = p
		out = append(out, f)
	}
	return out, errors.Join(errs...)
}

// next picks the new version: an explicit release-as, else the highest bump on latest.
func next(latest version, frags []fragment) (version, error) {
	level, explicit := "patch", ""
	for _, f := range frags {
		if bumpPriority[f.bump] > bumpPriority[level] {
			level = f.bump
		}
		if f.releaseAs != "" {
			if explicit != "" && explicit != f.releaseAs {
				return version{}, fmt.Errorf("conflicting release-as values %q and %q", explicit, f.releaseAs)
			}
			explicit = f.releaseAs
		}
	}
	if explicit == "" {
		return latest.bump(level), nil
	}
	v, _ := parseVersion(explicit)
	if !latest.less(v) {
		return version{}, fmt.Errorf("release-as %s must be greater than the latest tag v%s", v, latest)
	}
	return v, nil
}

// entry renders the changelog block for one release.
func entry(v version, date time.Time, frags []fragment) string {
	by := map[string][]string{}
	for _, f := range frags {
		by[f.typ] = append(by[f.typ], f.body)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## [%s] - %s\n", v, date.Format("2006-01-02"))
	for _, t := range typeOrder {
		if len(by[t]) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n### %s\n", t)
		for _, body := range by[t] {
			lines := strings.Split(body, "\n")
			fmt.Fprintf(&b, "- %s\n", lines[0])
			for _, l := range lines[1:] {
				if strings.TrimSpace(l) == "" {
					b.WriteString("\n")
				} else {
					fmt.Fprintf(&b, "  %s\n", l)
				}
			}
		}
	}
	return b.String()
}

// render inserts the entry above the first version heading and appends the compare link.
func render(changelog, ent string, v version, latestTag string) string {
	changelog = strings.ReplaceAll(changelog, "\r\n", "\n")
	if i := strings.Index(changelog, "\n## ["); i >= 0 {
		head, tail := changelog[:i+1], changelog[i+1:]
		changelog = strings.TrimRight(head, "\n") + "\n\n" + ent + "\n" + tail
	} else {
		head := strings.TrimRight(strings.Replace(changelog, placeholder, "", 1), "\n")
		changelog = head + "\n\n" + ent
	}
	link := fmt.Sprintf("[%s]: %s/compare/%s...v%s\n", v, repoURL, latestTag, v)
	if latestTag == "v0.0.0" {
		link = fmt.Sprintf("[%s]: %s/releases/tag/v%s\n", v, repoURL, v)
	}
	return strings.TrimRight(changelog, "\n") + "\n\n" + link
}
