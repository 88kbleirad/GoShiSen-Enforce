# Contributing to GoShiSen-Enforce

## Before you start

- Go ≥ 1.22 and `make`.
- Read `docs/design/architecture.md` and `docs/design/mvp-scope.md`: they are normative. Changes to behavior must update them in the same pull request.

## Workflow

```bash
git clone https://github.com/88kbleirad/GoShiSen-Enforce
cd GoShiSen-Enforce
make check          # fmt + vet + test + build — must pass before you push
```

CI (`.github/workflows/ci.yml`) runs the same gate plus `go test -race ./...`.

## Rules

1. **No placeholder code.** Do not add empty packages, stubs, or TODOs presented as features. A package lands with working code and tests, or not at all.
2. **No unrelated system changes.** The daemon must not touch nftables, routes, or sysctls except in the phase that owns that behavior.
3. **New dependencies need justification** in the PR description (why the standard library or an existing dependency is insufficient).
4. **Keep docs in sync.** README status, `docs/user-guide.md`, and `docs/developer-guide.md` must describe the repository as it is.
5. **Errors must be actionable**: name the field, the offending value, and the fix.

## Commit and pull requests

- Small, focused commits; one concern each.
- PR description: what changed, why, and the exact command you ran to verify it.
- Reference the phase/acceptance criterion from `mvp-scope.md` that the change satisfies.
