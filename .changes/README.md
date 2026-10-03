# Changesets

A PR declares what its change means for the next release here, instead of hand-editing
`CHANGELOG.md` (constant merge conflicts) or relying on commit-message parsing. Commits and PR
titles still follow Conventional Commits, but they do not drive versioning.

## Adding one

For any user-facing change, copy `TEMPLATE.md` to a new file in this directory (the name only
has to be unique and end in `.md`, e.g. `fix-period-key.md`) and fill it in:

```markdown
---
bump: patch
type: Fixed
---

`Weekly` used the calendar year instead of the ISO year around New Year.
```

- **bump**: `patch`, `minor` or `major`. The highest bump across all pending changesets wins.
  While the latest version is below `1.0.0`, `major` raises the **minor** number and
  `minor`/`patch` raise the patch number; the tool never reaches `1.0.0` by itself.
- **type**: `Added`, `Changed`, `Fixed`, `Removed`, `Deprecated` or `Security`
  ([Keep a Changelog](https://keepachangelog.com/en/1.0.0/) categories).
- **release-as** (optional): a deliberate version such as `0.1.0` or `1.0.0`. Must be greater
  than the latest tag; two changesets must not disagree.
- The body becomes the changelog bullet verbatim.

Skip this for internal-only changes (tests, CI, docs, refactors with no user-visible effect).
An invalid fragment fails the build; it is never silently skipped.

## What happens next

On every push to `main`, `.github/workflows/version.yml` looks for pending changesets. If there
are any it runs `go run ./internal/cmd/changeset version`, which rewrites `CHANGELOG.md`, deletes
the consumed files and prints the version. The workflow then opens or updates the single PR
`chore(release): vX.Y.Z` on `chore/next-release`. Merging it tags the merge commit and dispatches
`.github/workflows/release.yml`, which creates the GitHub release and warms the Go module proxy.

`go run ./internal/cmd/changeset lint` validates fragments locally; CI runs it on every PR.
The tool is stdlib-only Go inside the root module, so it adds no dependency.
