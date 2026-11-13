# Next phase backlog — prioritized by risk

Owner/poc: omp / 88kbleirad. Each item lists the risk it mitigates and
the verification hook that will close it. No item changes v0.2.1 assets.

| # | Priority | Item | Risk it mitigates | Proposed owner | Verification hook |
| --- | --- | --- | --- | --- | --- |
| 1 | High | Artifact signing / provenance (apk + release archives; proposal at `docs/design/signing-proposal.md`) | Distributions can be tampered with; apk currently installed with `--allow-untrusted` | 88kbleirad | Signed apk/archives; `apk verify` / GPG check in release workflow |
| 2 | High | Offload detect and disable — **implemented** (prompt 15): state machine (disabled/enabled/failed/unknown), `offload_policy=enforce` fail-closed disable (verified), `offload status/disable` CLI, `allow_bypass` diagnostic escape hatch, watchdog re-disable; live OpenWrt lab evidence in `docs/release/prompt15-offload-lab-evidence.md` | Established flows bypass the queue while offloading is active | 88kbleirad | User-visible disable path + docs; VM test with flow_offloading=1 — done. Remaining: hardware/paravirt bypass observability (backlog #3) |
| 3 | Medium | Flowtable bypass observability in the lab (TCG offloads 0 packets → bypass itself unobservable) | Gap between policy and enforcement unmeasured on real hardware | 88kbleirad | Hardware/paravirt target where flowtable offloads packets; counter evidence |
| 4 | Medium | arm64 runtime verification (binaries built/verified static only; runtime requires arm64 host) | Untested runtime behavior on arm64 | 88kbleirad | arm64 runner or qemu-user e2e; `go test` on arm64 host |
| 5 | Medium | Dangling `S12/K10goshisen-boot` symlinks on downgrade r4 → r2 | Downgrade leaves boot-guard rc.d links pointing at a missing script | 88kbleirad | prerm/upgrade hook fix + downgrade lab test |
| 6 | Low | Privileged NFQUEUE e2e reproducibility (no local kernel with NFQUEUE on dev/sandbox) | Gate is CI-only today; local debugging is heavyweight (Docker) | 88kbleirad | Documented container repro + optional kbuild config note |
