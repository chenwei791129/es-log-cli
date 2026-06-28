## ADDED Requirements

### Requirement: Test workflow on non-main pushes and PRs to main

The repository SHALL run the Go test suite via GitHub Actions on every push to
any branch other than `main`, and on every pull request targeting `main`, so
regressions are caught before a pull request is opened and again before it is
merged.

#### Scenario: Push to a feature branch triggers tests

- **WHEN** a commit is pushed to any branch whose name is not `main`
- **THEN** a workflow checks out the code, sets up Go using `go-version-file: go.mod`, and runs `go test ./...`
- **AND** the workflow does NOT run for pushes to `main`

#### Scenario: Pull request to main triggers tests

- **WHEN** a pull request targeting `main` is opened or updated
- **THEN** the same workflow checks out the code, sets up Go using `go-version-file: go.mod`, and runs `go test ./...`, so `go test` gates the merge into `main`

### Requirement: Pull request cross-platform build validation

The repository SHALL validate that the binary compiles for every supported
target platform on each pull request to `main`, so a release is never attempted
against code that fails to cross-compile.

#### Scenario: Pull request builds all target platforms

- **WHEN** a pull request targeting `main` is opened or updated
- **THEN** the binary is built once for each of `linux/amd64`, `linux/arm64`, `darwin/amd64`, and `darwin/arm64` with `CGO_ENABLED=0`
- **AND** a build failure on any single platform fails the workflow

### Requirement: Release-please manages releases on main

The repository SHALL run release-please in `release-type: go` mode on every push
to `main`, using the built-in `GITHUB_TOKEN`, to maintain a release pull request
and create GitHub releases from conventional commits.

#### Scenario: Push to main maintains the release PR

- **WHEN** a commit is pushed to `main`
- **THEN** the release-please action runs authenticated with `secrets.GITHUB_TOKEN`
- **AND** it opens or updates a release pull request reflecting the pending conventional commits

#### Scenario: Merging the release PR creates a release

- **WHEN** the release pull request is merged into `main`
- **THEN** release-please creates a GitHub release with the next version tag
- **AND** the release-created output signals the build-and-upload job to run

### Requirement: Versioned cross-platform release assets

When a release is created, the repository SHALL build a binary for every
supported target platform with the release version compiled in, and attach each
binary to the GitHub release as a downloadable asset.

#### Scenario: Release binaries are built and uploaded with the version injected

- **WHEN** a release is created for tag `vX.Y.Z`
- **THEN** a binary is built for each of `linux/amd64`, `linux/arm64`, `darwin/amd64`, and `darwin/arm64` from `./cmd/es-log` with `CGO_ENABLED=0`
- **AND** each build injects the tag via `-ldflags "-X github.com/chenwei791129/es-log-cli/internal/cmd.version=vX.Y.Z"` so the built binary reports `vX.Y.Z` from its version command
- **AND** each binary is uploaded to the `vX.Y.Z` release as an asset named for its platform
