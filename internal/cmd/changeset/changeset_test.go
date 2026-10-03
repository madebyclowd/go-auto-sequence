package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseFragment(t *testing.T) {
	good := "---\nbump: minor # c\ntype: Added\n---\n\nHello\n  more\n"
	f, err := parseFragment("a.md", good)
	if err != nil || f.bump != "minor" || f.typ != "Added" || f.body != "Hello\n  more" {
		t.Fatalf("got %+v, %v", f, err)
	}
	bad := map[string]string{
		"no frontmatter": "hello",
		"unterminated":   "---\nbump: patch\n",
		"bad bump":       "---\nbump: huge\ntype: Fixed\n---\nx",
		"bad type":       "---\nbump: patch\ntype: Oops\n---\nx",
		"empty body":     "---\nbump: patch\ntype: Fixed\n---\n\n",
		"unknown key":    "---\nbump: patch\ntype: Fixed\nfoo: 1\n---\nx",
		"bad release-as": "---\nbump: patch\ntype: Fixed\nrelease-as: 1.0\n---\nx",
	}
	for name, in := range bad {
		if _, err := parseFragment("a.md", in); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestNext(t *testing.T) {
	frag := func(bump, as string) fragment { return fragment{bump: bump, releaseAs: as} }
	cases := []struct {
		latest string
		frags  []fragment
		want   string
		fail   bool
	}{
		{"v0.0.0", []fragment{frag("patch", "")}, "0.0.1", false},
		{"v0.3.2", []fragment{frag("minor", "")}, "0.3.3", false},
		{"v0.3.2", []fragment{frag("patch", ""), frag("major", "")}, "0.4.0", false},
		{"v1.2.3", []fragment{frag("patch", "")}, "1.2.4", false},
		{"v1.2.3", []fragment{frag("minor", ""), frag("patch", "")}, "1.3.0", false},
		{"v1.2.3", []fragment{frag("major", "")}, "2.0.0", false},
		{"v0.9.0", []fragment{frag("patch", "1.0.0")}, "1.0.0", false},
		{"v0.0.0", []fragment{frag("patch", "0.1.0")}, "0.1.0", false},
		{"v0.2.0", []fragment{frag("patch", "0.1.0")}, "", true},
		{"v0.0.0", []fragment{frag("patch", "0.1.0"), frag("patch", "0.2.0")}, "", true},
	}
	for _, c := range cases {
		lv, _ := parseVersion(c.latest)
		got, err := next(lv, c.frags)
		if (err != nil) != c.fail || (!c.fail && got.String() != c.want) {
			t.Errorf("%s %+v: got %v, %v want %s", c.latest, c.frags, got, err, c.want)
		}
	}
}

func TestRenderFirstAndSecondRelease(t *testing.T) {
	date := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	intro := "# Changelog\n\nIntro.\n\nNothing has been released yet.\n"
	first := entry(version{0, 1, 0}, date, []fragment{
		{typ: "Fixed", body: "fix one\n  detail"},
		{typ: "Added", body: "add one"},
	})
	want1 := "## [0.1.0] - 2026-10-03\n\n### Added\n- add one\n\n### Fixed\n- fix one\n    detail\n"
	if first != want1 {
		t.Fatalf("entry:\n%q\nwant\n%q", first, want1)
	}
	out := render(intro, first, version{0, 1, 0}, "v0.0.0")
	if strings.Contains(out, "Nothing has been released") || !strings.Contains(out, "[0.1.0]: https://github.com/madebyclowd/go-auto-sequence/releases/tag/v0.1.0") {
		t.Fatalf("first release:\n%s", out)
	}
	second := entry(version{0, 1, 1}, date, []fragment{{typ: "Fixed", body: "later"}})
	out2 := render(out, second, version{0, 1, 1}, "v0.1.0")
	if strings.Index(out2, "## [0.1.1]") > strings.Index(out2, "## [0.1.0]") {
		t.Fatalf("new entry must sit above the old one:\n%s", out2)
	}
	if !strings.HasSuffix(out2, "[0.1.1]: https://github.com/madebyclowd/go-auto-sequence/compare/v0.1.0...v0.1.1\n") {
		t.Fatalf("footer:\n%s", out2)
	}
}
