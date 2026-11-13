# GoShiSen-Enforce — Developer Guide

## Prerequisites

- Go ≥ 1.25 (matches `go.mod`; CI and Docker use Go 1.25)
- Linux (for the eventual lab namespace work, root)
- `make` for the standard targets

## Build, test, verify

```bash
make fmt        # verify gofmt cleanliness (fails on unformatted files)
make vet        # go vet ./...
make test       # go test ./...
make build      # build bin/goshisen with version ldflags
make check      # fmt + vet + test + build (the full local gate)
make run        # build and run with configs/goshisen.yaml
make clean      # remove bin/
make tidy       # go mod tidy
make package     # build all release archives (amd64+arm64) + checksums + SBOM
make verify-package  # validate the archives: arch, static linkage, version, checksums, SBOM
make sbom       # (re)generate the SPDX SBOM (requires syft)
make help       # list all targets
```

CI (`.github/workflows/ci.yml`) runs the same gate on Ubuntu: `go mod verify`, gofmt check, `go vet`, `go test -race`, `go build`, and a CLI smoke test. A `package` job additionally builds + verifies the release archives on every PR (artifacts uploaded for inspection, never published). The `release` workflow (`.github/workflows/release.yml`) runs only on `v*` tags: it builds + verifies and attaches artifacts to a **draft** release for the tag — publication remains a manual, authorized step.

## Repository layout

```
cmd/goshisen/        main package (thin CLI dispatch)
internal/app/        daemon lifecycle: policy load, hook install, queue loop, API/metrics, shutdown
internal/cli/        flag parsing, help/version, command validation, policy subcommand flags
internal/config/     config model, YAML decoding (strict), precedence merging, redaction, read-only host checks
internal/policy/     policy YAML parser, immutable snapshots, atomic replacement, stateful engine
internal/packet/     NFQUEUE backend (direct nfnetlink_queue over mdlayher/netlink) + IPv4/TCP/UDP parser
internal/hook/       nftables hook provisioning (nft binary; current namespace only)
internal/store/      durable versioned policy store (atomic active pointer)
internal/api/        REST API (localhost, bearer auth, version CAS, audit, liveness/readiness)
internal/metrics/    bounded-cardinality Prometheus registry
configs/             sample configuration
policies/default/    deny-all policy example
policies/examples/   worked policy example
api/openapi.yaml     OpenAPI spec matching the API implementation
scripts/lab.sh       isolated netns lab (up/down/status)
tests/integration/   real-traffic integration suite + probe helper (root-gated)
docs/design/         architecture.md, mvp-scope.md (normative design)
docs/                user guide, this guide
.github/workflows/   CI (build + package + privileged integration jobs); release (tag-triggered, draft-only)
```

NAT lives in `internal/hook` (kernel-side nftables chains in the app-owned table); `internal/nat` does not exist — NAT has no separate package. The hook package renders the complete ruleset (filter + NAT) as one atomic `nft -f` transaction.

## Conventions

- **No fake progress.** A package exists only when it has real code and tests. The daemon never claims features it does not implement.
- **No system modifications outside the owning phase.** The hook package only ever modifies `table ip goshisen` in the current namespace (`nft`); the host firewall is never listed, flushed, or edited. The labs' nftables state lives inside `gsh_fw` and disappears with its namespace.
- **Every packet gets a verdict.** The NFQUEUE backend always sends ACCEPT or DROP; anomalies (parse failure, panic, missing decider) are DROP.
- **stdlib-first.** External dependencies are deliberate; currently `gopkg.in/yaml.v3` and `github.com/mdlayher/netlink` (pinned) only.
- **Precedence is explicit.** Config layers: defaults < file < environment < flags. Every field records its source.
- **Strict parsing.** YAML decoding uses `KnownFields(true)`; typos are load errors.
- **Clear errors.** User-facing errors name the field, the offending value, and the fix.
- **Signals.** SIGINT/SIGTERM stop the queue and exit 0; the hook remains (fail-closed).

## Testing

- `internal/config`: precedence layering, strict YAML, env bool parsing, validation errors, source summary, redaction, `~` expansion, `queue_maxlen` (default/file/env/flag + bounds).
- `internal/cli`: help/version, flag parsing, subcommand validation, `policy simulate` target parsing, `--queue-maxlen`.
- `internal/app`: logger text/JSON, level filtering, lifecycle with the fake queue (policy-before-hook, graceful shutdown, queue-open failure), queue-overflow watcher (deltas, rate-limited warning, unavailable-stats exit), atomic publish (`publish` order, malformed/partial-failure keeps old policy, concurrent publishes serialize to distinct versions, IPv4/IPv6 reload, end-to-end SIGHUP reload while running), `-race` clean.
- `internal/policy`: CIDR boundary, port boundary, overlapping rules/ordering, default deny, invalid configs, snapshot immutability, atomic replace, concurrency, stateful semantics (accepted-set, five-tuple-not-enough, reload-keeps-established).
- `internal/packet`: parser (valid TCP/UDP, options, truncations, fragments, unsupported protocols, no-panic sweep); backend `evaluate` verdicts (accept/drop/parse-error/panic); `applyCTInfo` mapping; counters; kernel queue-stats parser (`qstats.go`); fail-closed invariant (never requests `NFQA_CFG_F_FAIL_OPEN`).
- `internal/packet` fuzz: `make fuzz` (30s) — `FuzzParse` clean.
- `internal/store`: version validation, atomic writes (no stray temp files), active-pointer integrity.
- `internal/api`: auth (missing/valid + audit + no token leak), invalid-policy preserves active, version conflict, concurrent apply (exactly one 201), restart recovery, activate/rollback, body limit, readiness, metrics, policy-update outcome metric, shared-mutex concurrency with daemon publishes, mid-publish kill recovery (torn version never activates); all under `-race`.
- `internal/metrics`: bounded label cardinality, no secret/flow-derived labels, histogram/gauge/counter output, queue-overflow counter (bounded `cause` enum).
- `tests/integration`: real-traffic scenarios (Phase 3 six + Phase 4 six + Phase 6 five + Phase 7 overflow). **Root-gated**: `checkPrereqs` skips with an explicit reason. Run with `sudo make integration` (CI has a dedicated privileged job).
- CLI smoke tests run in CI (help/version, missing-config error, invalid-policy error).

## Versioning

`make build` stamps `internal/cli.Version` and `internal/app.Version` with `git describe --tags --always --dirty`. Untagged builds report `dev`.

## Policies

- Run `make check` before committing; CI runs the same gate.
- Tests must pass with `go test -race ./...` (CI does this).
- `gofmt` is enforced; run `make fmt-fix` when `make fmt` fails.
