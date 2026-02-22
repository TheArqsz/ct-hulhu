# Contributing to ct-hulhu

## Requirements

- Go 1.24+
- GNU Make (optional, for convenience targets)

## Build and test

```bash
git clone https://github.com/TheArqsz/ct-hulhu.git
cd ct-hulhu

# Build with version from git tags
make build

# Run tests
make test

# Vet (static analysis)
make vet
```

`make build` injects the version from `git describe --tags` via ldflags, so the binary knows its version at runtime. Without tags, it defaults to `dev`.

## Project structure

```
cmd/ct-hulhu/         Entry point (main.go)
internal/
  runner/             CLI parsing, orchestration, logging, state management
  ctlog/              CT log client, adaptive worker pool
  loglist/            Google Chrome CT log list parser
  certparser/         MerkleTreeLeaf + X.509 parsing, domain filtering
  output/             Deduplicated text and JSONL output
  updater/            Self-update via GitHub releases
```

Everything is under `internal/` - there's no public Go API, this is a CLI tool.

## Conventions

This project uses [Conventional Commits](https://www.conventionalcommits.org/). The release workflow generates changelogs and determines version bumps from commit prefixes:

- `feat:` - new feature (minor bump)
- `fix:` - bug fix (patch bump)
- `ci:` - CI/CD changes (no bump)
- `docs:` - documentation (no bump)
- `refactor:` - code restructuring (no bump)
- `test:` - adding/fixing tests (no bump)

## Release process

Automated via GitHub Actions:

1. PR gets merged to `main`
2. The release workflow runs `go vet` + `go test -race` as a gate
3. If tests pass, it scans commits since the last tag
4. If there are `feat:` or `fix:` commits, it bumps the version, updates `CHANGELOG.md`, and creates a git tag
5. GoReleaser builds static binaries for linux/darwin/windows (amd64/arm64)
6. GitHub release is created with the binaries, checksums, and changelog

No manual tagging needed. Just write conventional commit messages and merge.

## Adding a new output field

1. Parse the data in `internal/certparser/parser.go` (add to `buildResult`)
2. Add the field to `CertResult` in `internal/ctlog/types.go`
3. Add output handling in `internal/output/output.go`
4. Add the field name to the `-f` flag validation in `internal/runner/options.go`

## Running against a real log

For local testing against a real CT log:

```bash
# Small test: grab 100 entries from the end of a Google log
go run ./cmd/ct-hulhu/ -lu https://ct.googleapis.com/logs/us1/argon2025h1/ -from-end -n 100 -v

# With domain filter
go run ./cmd/ct-hulhu/ -lu https://ct.googleapis.com/logs/us1/argon2025h1/ -from-end -n 10000 -d google.com -v
```
