# Contributing

Thanks for considering a contribution to go-auto-sequence.

## Development setup

You need Go 1.22 or newer (the module's minimum version).

```bash
git clone https://github.com/madebyclowd/go-auto-sequence.git
cd go-auto-sequence
go build ./...
```

## Running checks locally

```bash
go vet ./...                     # static checks
golangci-lint run                # lint (see .golangci.yml)
go test -race -shuffle=on ./...  # tests with the race detector
```

All of these run in CI on every push and pull request; a PR won't be merged unless they pass.

The root module has **no dependencies** and must stay that way. Anything that needs a
third-party package belongs in a separate module.

## Pull requests

- Target the `main` branch.
- Add or update tests for any behavior change.
- Keep PRs focused: one logical change per PR.
- Use [Conventional Commits](https://www.conventionalcommits.org/) for commit messages and
  PR titles (`feat:`, `fix:`, `chore:`, `docs:`, `refactor:`, `test:`, `ci:`).

## Reporting bugs

Use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.md). Include your Go version,
OS, database and driver, and a minimal reproduction.

## Reporting security vulnerabilities

Do not open a public issue; see [SECURITY.md](SECURITY.md).
