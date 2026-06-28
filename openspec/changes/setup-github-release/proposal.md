## Why

`es-log` is moving from a private repository to a public GitHub project as a
personal side project. To make it consumable, the project needs an open-source
license, automated cross-platform release builds, and installation docs that
point users at pre-built binaries instead of requiring a local Go toolchain.

## What Changes

- Create the public GitHub repository `chenwei791129/es-log-cli` with the `gh`
  CLI (it does not exist yet), set it as the `origin` remote, and push the
  current (history-rewritten) `main` branch so the workflows below become active.
- Add an MIT `LICENSE` file (`Copyright (c) 2026 Chen-Wei Sun`) and rewrite the
  README `## License` section to reference it.
- Add three GitHub Actions workflows under `.github/workflows/`:
  - `go-test.yml` — run `go test` on every push to a non-`main` branch.
  - `pr-build.yml` — cross-platform build validation on pull requests to `main`.
  - `release-please.yml` — on push to `main`, run release-please (`release-type: go`)
    to manage release PRs; when a release is created, build cross-platform
    binaries and upload them as release assets.
- Use the built-in `secrets.GITHUB_TOKEN` for release-please (no PAT, since
  `main` has no branch protection).
- Inject the version via `-X github.com/chenwei791129/es-log-cli/internal/cmd.version=<tag>`
  building `./cmd/es-log` (this project's entry point and version symbol differ
  from the reference repo's root `main.version`).
- Target platforms: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.
- Rewrite the README `## Installation` section to lead with downloading a
  pre-built binary from GitHub Releases (`curl -L` examples), demoting
  `make build` from source to a secondary path under Development.

## Non-Goals

- No Windows build target and no `goversioninfo`/`versioninfo.json` manifest
  embedding (the reference repo's Windows handling is intentionally dropped).
- No PAT secret / branch-protection setup.
- No `release-please-config.json` or `.release-please-manifest.json` — the
  action runs in simple `release-type: go` mode with defaults.
- No package-manager distribution (Homebrew tap, `go install` instructions,
  apt/deb) — only direct binary download from Releases.
- No checksum/signature (`.sha256`, cosign) generation for release assets.

## Capabilities

### New Capabilities

- `release-automation`: GitHub Actions pipeline that tests non-main pushes,
  validates cross-platform builds on PRs, and produces versioned cross-platform
  release binaries via release-please.

### Modified Capabilities

(none)

## Impact

- Affected specs: new `release-automation`
- Repository: a new public GitHub repo `chenwei791129/es-log-cli` is created and
  set as the `origin` remote; `main` is pushed for the first time.
- Affected code:
  - New:
    - LICENSE
    - .github/workflows/go-test.yml
    - .github/workflows/pr-build.yml
    - .github/workflows/release-please.yml
  - Modified:
    - README.md
  - Removed: (none)
