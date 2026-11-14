# Next phase backlog — prioritized by risk

Owner/poc: omp / 88kbleirad. Each item lists the risk it mitigates and
the verification hook that will close it. No item changes v0.2.1 assets.

| # | Priority | Item | Risk it mitigates | Proposed owner | Verification hook |
| --- | --- | --- | --- | --- | --- |
| 1 | High | Artifact signing / provenance (apk + release archives) — **implemented**: `docs/release/signing-design.md`, trust anchor `keys/goshisen-release.pub.pem` | Distributions can be tampered with; apk currently installed with `--allow-untrusted` transition exception | 88kbleirad | Signed apk/archives; `apk verify` / GPG check in release workflow. Remaining: native `usign` apk verification (removes `--allow-untrusted`) |
| 2 | High | Offload detect and disable clearly (current: detect/warn only; `offload_policy` modes do not disable offloading) | Established flows bypass the queue while offloading is active | 88kbleirad | User-visible disable path + docs; VM test with flow_offloading=1 |
| 3 | Medium | Flowtable bypass observability in the lab (TCG offloads 0 packets → bypass itself unobservable) | Gap between policy and enforcement unmeasured on real hardware | 88kbleirad | Hardware/paravirt target where flowtable offloads packets; counter evidence |
| 4 | Medium | arm64 runtime verification (binaries built/verified static only; runtime requires arm64 host) | Untested runtime behavior on arm64 | 88kbleirad | arm64 runner or qemu-user e2e; `go test` on arm64 host |
| 5 | Medium | Dangling `S12/K10goshisen-boot` symlinks on downgrade r4 → r2 | Downgrade leaves boot-guard rc.d links pointing at a missing script | 88kbleirad | prerm/upgrade hook fix + downgrade lab test |
| 6 | Low | Privileged NFQUEUE e2e reproducibility — **implemented** (prompt 19): single `scripts/e2e-runner.sh` entrypoint (preflight → manifest → pinned provisioning → boot → e2e → trap teardown → failure artifacts); `scripts/e2e-env-manifest.sh`; hardened lab `up`/`down` (stale-resource-safe); CI job uses the runner + uploads artifacts; unprivileged → BLOCKED. Evidence: `docs/release/prompt19-e2e-reproducibility-evidence.md` | Gate is CI-only today; local debugging is heavyweight (Docker) | 88kbleirad | Documented reproducible command — done (CI + local privileged + unprivileged-blocked matrix) |
