# Contributing to tdarr-exporter

Thanks for your interest in improving `tdarr-exporter`. This guide covers local
development, the checks CI runs, and the commit/PR conventions the project
depends on.

## Prerequisites

- **Go 1.26+** (the module targets `go 1.26`; CI uses the toolchain pinned in
  `go.mod`).
- **[go-task](https://taskfile.dev)** — local workflows are defined in
  `Taskfile.yml`, not a `Makefile`. Run `task --list` for the full set; the task
  definitions are the source of truth.
- **Docker** — required for `task lint` (golangci-lint runs in a pinned
  container matching CI) and for the image build tasks.

## Local development

The exporter polls a Tdarr instance's HTTP API on each `/metrics` scrape, so
running it locally needs a reachable Tdarr URL.

1. Create `.local/local.env` with at least `TDARR_URL` set to your instance.
2. `task dev` — live-reload loop (via `air`), loading env from
   `.local/local.env`.
3. `task curl` — hit the local `/metrics` endpoint (port 9090) to see the
   exported series.

## Testing

- `task test` — run all tests.
- `task test:race` — all tests with the race detector.
- `task test:cover` — tests with a per-function coverage summary.
- Run a single test directly:
  `go test ./internal/collector/ -run TestName -count=1 -v`.

Test conventions used throughout the repo:

- Test observable behavior (inputs → outputs, side effects, error conditions),
  not internal implementation details.
- Deterministic and parallelizable: no `time.Sleep`-based synchronization, no
  reliance on map iteration order, no shared mutable state between tests.
- Table-driven subtests (`t.Run`) for input variations of the same behavior.
- Mock at system boundaries (network, disk) using the in-repo fakes, not between
  internal packages.

## CI gates

Before pushing, run the aggregate gate:

```bash
task ci
```

`task ci` mirrors CI exactly and runs, in order:

1. `go fmt` — must leave no diff.
2. `go mod tidy` — must leave no diff.
3. `golangci-lint` (pinned via Docker).
4. `go test ./... -race` with coverage.

A change should land `task ci` green locally before it goes up for review.

## Commit conventions

The project uses [Conventional Commits](https://www.conventionalcommits.org/) —
**release-please** parses them to generate `CHANGELOG.md`, bump the version, and
cut tags. Getting the type right is what determines the release.

- `feat:` — a new feature (MINOR bump).
- `fix:` — a bug fix (PATCH bump).
- `refactor:`, `chore:`, `ci:`, `docs:`, `test:` — no version bump.
- Append `!` to any type (e.g. `feat!:`) for a breaking change (MAJOR bump). A
  `BREAKING CHANGE:` footer does the same. Use these deliberately — either one
  forces a major release.

Do **not** hand-edit `CHANGELOG.md` or version files; release-please owns them.

## Pull requests

- Work on a branch; include the issue number where one exists.
- **Get approval before opening/pushing a PR.**
- PR descriptions should state what changed (facts, plus any assumptions or
  unknowns) and why, and include a test plan / verification steps. Skip
  auto-generated stats (lines changed, file counts).
- Keep `examples/` (the Grafana dashboard and `alerts.yaml`) in sync when you
  change metric names, types, or semantics.

## Metric semantics

Several `tdarr_*` metric behaviors look like bugs but are intentional (Tdarr is
closed-source; the behaviors were derived empirically). Before changing any
metric name, type, or scale, read `docs/metrics-internals.md` — it holds the
evidence and the rationale. Breaking-change history lives in `README.md`.
