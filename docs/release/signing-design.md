# Artifact Signing & Provenance — Design & Migration (Prompt 12)

**Status:** implemented for verification gate + tooling (test keys); production
key bootstrap is a separate, approved step.
**Applies to:** release artifacts — `goshisen-linux-amd64.tar.gz`,
`goshisen-linux-arm64.tar.gz`, `checksums.txt`, `sbom.spdx.json`, and the OpenWrt
`.apk` (provenance only until native `usign` is added).
**Does NOT change:** tag `v0.2.1`, its assets, or any runtime binary/policy engine.

---

## 1. Trust model

- **Algorithm:** Ed25519 (OpenSSL `pkeyutl -sign/-verify`, raw sign). Available in
  every ubuntu-24.04 runner and on user machines (`openssl`).
- **Key layout:**
  - `dist/public_key.pem` — the release public key, committed to the repository
    (verification material is public by design).
  - Private key: **never committed**. On the release workflow it comes from a
    GitHub Actions secret (`GH_RELEASE_SIGNING_KEY`) or an external vault; for PRs
    and tests, a throwaway test key is generated inside the job and never leaves it.
- **Trust anchor:** the public key is published (a) in this repository,
  (b) in the release notes, and (c) out-of-band by the maintainers. First-install
  trust is anchored independently of the artifact channel.
- **Rotation:** public key lives at a stable path; a rotating key would use
  key-id suffixes (`public_key-2026.pem`). Rotation policy: sign with the new key
  for a release, keep old key for verification of older releases, document the
  transition in the evidence index. Rollback = keep verifying with the previous key.

## 2. Per-artifact signature & provenance

For every release artifact `A` we produce:

| File | Content |
| --- | --- |
| `A.sig` | Ed25519 signature (64 bytes) over `A` |
| `A.sig.pem` | copy of the public key used (for offline verification) |
| `provenance.json` | source commit, tag, builder (workflow), toolchain (Go version), per-artifact sha256, build timestamp, target architecture |
| `provenance.json.sig` | signature over the provenance file |

`checksums.txt` is itself signed; the SBOM is signed like any artifact.

## 3. Provenance record (minimal)

```json
{
  "version": 1,
  "artifact": "goshisen-linux-amd64.tar.gz",
  "source_commit": "<full sha>",
  "tag": "v0.2.2",
  "builder": "release.yml / package job / @ 8e007a5d",
  "toolchain": "go1.25.x / CGO_ENABLED=0 / linux/amd64",
  "target_arch": "amd64",
  "build_timestamp": "2026-11-13T00:00:00Z",
  "sha256": "<artifact hash>"
}
```

## 4. Verification gate (fail closed) — `scripts/verify-signatures.sh`

Run before any upload (CI step) and locally. Exits non-zero unless:

1. every artifact in the manifest has a signature file (`A.sig`), and
2. every signature verifies with the supplied public key, and
3. the provenance file exists, is signed by the same key, and its `sha256`
   matches the actual artifact bytes, and
4. the source commit recorded in provenance matches the commit being built
   (stale-provenance rejection), and
5. for archives: the embedded binary architecture matches the filename
   (`amd64`/`arm64`) — architecture mismatch rejection.

Errors are distinct: `missing-signature`, `bad-signature`,
`provenance-mismatch`, `stale-provenance`, `arch-mismatch`, `missing-key`.

## 5. CI/CI integration (no production secrets on PRs)

- **`verify-signatures` job (new, ci.yml):** runs on every PR with a **test key
  generated in-flow** and stored in `$RUNNER_TEMP`. It builds the archives, signs
  them, then (a) runs the verifier against the correct key → must pass, and
  (b) tampers with a copy of `dist/` and asserts the verifier exits non-zero with
  a `bad-signature` error → must fail. This keeps the gate honest without touching
  secrets.
- **Go suite (`tests/package/signing_artifacts_test.go`):** covers the remaining
  failure modes end-to-end through the real scripts with throwaway keys, asserting
  the documented exit codes: wrong key → 4, tampered artifact → 4, missing
  signature → 3, stale provenance → 6, provenance hash mismatch → 5,
  missing provenance → 8, missing public key → 2, arch mismatch → 7.
- **release.yml:** a `sign` job (tag-only) will later import the *production*
  secret key, sign `dist/*`, verify, then upload. Guarded by boolean `sign: true`
  that remains **false** until key bootstrap is approved (so no accidentally
  signed upload happens). Until then the release workflow stays exactly as today.

## 6. OpenWrt `.apk`

- The APK is a zip; we sign the raw `.apk` blob with the same Ed25519 key
  (`<apk>.sig`), which verifies offline (`openssl pkeyutl -verify`).
- Native `apk` verification requires `usign` keys under `/etc/apk/keys/`
  (`<keyname>-<fingerprint>.pub`). That is a follow-up (backlog #1), and it
  will **remove `--allow-untrusted` from the production install path**.
- Today `--allow-untrusted` appears only in (a) `scripts/openwrt-image-prep.sh`
  installing *kmods* whose checksums were already verified with
  `sha256sum -c --strict` before install (lab path, not release), and
  (b) documentation. No production release path currently ships an unsigned
  goshisen APK install instruction as a default.

## 7. Migration plan

1. This PR: tooling (`sign-artifacts.sh`, `verify-signatures.sh`), provenance
   writer, public-key fixture, CI `verify-signatures` job (test keys), unit tests
   for all failure modes, design note. → merges without touching release.yml
   production secrets.
2. Approved follow-up: maintainers generate a production Ed25519 key, store the
   private key as `GH_RELEASE_SIGNING_KEY`, commit the public key, set `sign: true`
   on tags, and update docs (`docs/installation.md`, `docs/openwrt.md`) to show
   verification without `--allow-untrusted`.
3. Optional later: native OpenWrt `usign` key for `apk` (backlog #1).

## 8. Secrets safety

- Test keys are generated per-job and discarded; their public halves are fixtures.
- No private key material is committed, logged, or embedded in artifacts.
- The production signing secret only ever exists as a GitHub Actions secret /
  vault value and is referenced, never echoed.
