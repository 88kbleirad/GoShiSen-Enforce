#!/usr/bin/env bash
# apk-build-manifest-test.sh — regression tests for
# scripts/check-apk-build-manifest.sh and for the pin-gate hook that runs
# it (scripts/release-pin-gate.sh step 9).
#
# The fixtures are GENERATED from the checked-out commit, so they cannot
# drift from the recipe. Deliberately, the generator replays the recipe
# with tools the checker does NOT use (`patch(1)` + `sed(1)` instead of
# `git apply` + python), so a passing "valid" case is a real cross-check
# of the replay logic rather than a tautology.
#
# Every negative case must fail AND name the right reason.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CHECK="$ROOT/scripts/check-apk-build-manifest.sh"
GATE="$ROOT/scripts/release-pin-gate.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

command -v patch >/dev/null 2>&1 || { echo "FAIL: patch(1) required" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "FAIL: jq required" >&2; exit 1; }

fail() { echo "FAIL: $*" >&2; exit 1; }

POLICY="$ROOT/packaging/go-toolchain.env"
policy_get() { sed -nE "s/^$1=[[:space:]]*//p" "$POLICY" | head -n1; }
GEN_SHA="$(sha256sum "$ROOT/scripts/build-apk-release.sh" | cut -d' ' -f1)"
SDK_PIN="$(sed -nE 's/^[[:space:]]*image:[[:space:]]*(openwrt\/sdk@sha256:[0-9a-f]{64}).*/\1/p' \
  "$ROOT/.github/workflows/apk-sign.yml" | head -n1)"
[ -n "$SDK_PIN" ] || fail "cannot read the SDK pin from apk-sign.yml"

# gen_manifest <repo> <commit> <version> <outdir>
# Writes <outdir>/{build-manifest-A.json,goshisen-<ver>-r1.apk,daemon-A.bin}
# using an INDEPENDENT replay of recipe + overlay + substitution.
gen_manifest() {
  local repo="$1" commit="$2" version="$3" out="$4"
  mkdir -p "$out/replay"
  git -C "$repo" archive "$commit" openwrt packaging/apk-build | tar -x -C "$out/replay"

  # Committed (pre-overlay) hashes — this is what the manifest records.
  local recipe_json
  recipe_json="$(cd "$out/replay" && for f in $(find openwrt -type f | sort); do
      printf '%s\n%s\n' "$f" "$(sha256sum "$f" | cut -d' ' -f1)"
    done | jq -Rn '[inputs] | [range(0; length; 2) as $i | {key: .[$i], value: .[$i+1]}] | from_entries')"

  (cd "$out/replay" && patch -p1 -s < packaging/apk-build/no-golang-host.patch) \
    || fail "generator: overlay did not apply"
  sed -E -i "s/^PKG_VERSION:=.*/PKG_VERSION:=$version/" "$out/replay/openwrt/Makefile"

  local overlay_sha cfg_sha mk_sha
  overlay_sha="$(sha256sum "$out/replay/packaging/apk-build/no-golang-host.patch" | cut -d' ' -f1)"
  cfg_sha="$(sha256sum "$out/replay/packaging/apk-build/sdk-x86_64.config" | cut -d' ' -f1)"
  mk_sha="$(sha256sum "$out/replay/openwrt/Makefile" | cut -d' ' -f1)"
  local src_ver
  src_ver="$(sed -nE 's/^PKG_SOURCE_VERSION:=(.*)/\1/p' "$out/replay/openwrt/Makefile" | head -1)"

  # Synthetic, clearly-fake artifacts (no signing, no SDK container).
  mkdir -p "$out/apkx/usr/sbin"
  printf '#!/bin/sh\necho "goshisen %s"\n' "$version" > "$out/apkx/usr/sbin/goshisen"
  (cd "$out/apkx" && tar -czf "$out/goshisen-$version-r1-A.apk" usr/sbin/goshisen)
  cp "$out/apkx/usr/sbin/goshisen" "$out/daemon-A.bin"
  chmod +x "$out/daemon-A.bin"

  jq -n \
    --arg gensha "$GEN_SHA" \
    --arg commit "$commit" --arg version "$version" --arg srcver "$src_ver" \
    --argjson recipe "$recipe_json" --arg ovlsha "$overlay_sha" --arg cfgsha "$cfg_sha" \
    --arg mksha "$mk_sha" --arg sdk "$SDK_PIN" \
    --arg gover "$(policy_get GO_TOOLCHAIN_VERSION)" \
    --arg gotsha "$(policy_get GO_TOOLCHAIN_TARBALL_SHA256)" \
    --arg gobin "$(policy_get GO_TOOLCHAIN_BIN_GO_SHA256)" \
    --arg apksha "$(sha256sum "$out/goshisen-$version-r1-A.apk" | cut -d' ' -f1)" \
    --arg paysha "$(sha256sum "$out/daemon-A.bin" | cut -d' ' -f1)" \
    '{
      manifest_version: 1, created_utc: "2026-01-01T00:00:00Z", label: "A",
      generator_sha256: $gensha,
      generator: "tests/ci/apk-build-manifest-test.sh",
      source: {commit: $commit, commit_subject: "fixture", tree_clean: true},
      pkg: {name: "goshisen", version: $version, release: "1",
            source_version: $srcver, mirror_hash: "0000000000000000000000000000000000000000000000000000000000000001",
            mirror_hash_derived: true},
      recipe: {dir: "openwrt", committed_sha256: $recipe,
               overlay: {path: "packaging/apk-build/no-golang-host.patch", sha256: $ovlsha},
               sdk_config: {path: "packaging/apk-build/sdk-x86_64.config", sha256: $cfgsha},
               makefile_final_sha256: $mksha,
               substitutions: [{key: "PKG_VERSION", value: $version, rule: ("s/^PKG_VERSION:=.*/PKG_VERSION:=" + $version + "/")}]},
      build: {sdk_image: $sdk, goarch: "amd64",
              go: {version: $gover, tarball_sha256: $gotsha, bin_go_sha256: $gobin}},
      artifacts: {apk: {name: ("goshisen-" + $version + "-r1.apk"), file: ("goshisen-" + $version + "-r1-A.apk"), sha256: $apksha, size: 1, signed: false},
                  payload: {path: "usr/sbin/goshisen", sha256: $paysha, version: ("goshisen " + $version)}}
    }' > "$out/build-manifest-A.json"
}

expect_fail() { # expect_fail <manifest> <needle> [extra args...]
  local manifest="$1" needle="$2"; shift 2
  local out rc=0
  out="$("$CHECK" --manifest "$manifest" "$@" 2>&1)" || rc=$?
  [ "$rc" -ne 0 ] || fail "$(basename "$manifest"): was ACCEPTED (expected failure: $needle)"
  grep -qF "$needle" <<<"$out" \
    || fail "$(basename "$manifest"): failed for the wrong reason; wanted '$needle', got: $out"
  echo "  ok: rejected for the right reason ($needle)"
}

COMMIT="$(git -C "$ROOT" rev-parse HEAD)"
OUT="$TMP/real"
gen_manifest "$ROOT" "$COMMIT" "0.2.9-test" "$OUT"

# --- valid ---------------------------------------------------------------
echo "== valid manifest must pass (cross-checked replay) =="
"$CHECK" --manifest "$OUT/build-manifest-A.json" --commit "$COMMIT" \
  --apk "$OUT/goshisen-0.2.9-test-r1-A.apk" --payload "$OUT/daemon-A.bin" \
  | tail -3

# --- missing manifest ----------------------------------------------------
echo "== missing manifest =="
rc=0
out="$("$CHECK" --manifest "$TMP/absent.json" 2>&1)" || rc=$?
[ "$rc" -ne 0 ] || fail "absent manifest was ACCEPTED"
grep -qF "not found" <<<"$out" || fail "absent manifest: wrong reason: $out"
echo "  ok: rejected for the right reason (not found)"

# --- wrong builder hash --------------------------------------------------
echo "== manifest claims a different build script =="
jq '.generator_sha256 = "9999999999999999999999999999999999999999999999999999999999999999"' \
  "$OUT/build-manifest-A.json" > "$TMP/wrong-gen.json"
expect_fail "$TMP/wrong-gen.json" "produced by a different build script"

# --- malformed / missing field ------------------------------------------
echo "== malformed JSON and missing field =="
echo '{ not json' > "$TMP/bad.json"
expect_fail "$TMP/bad.json" "not valid JSON"
jq 'del(.artifacts.apk.sha256)' "$OUT/build-manifest-A.json" > "$TMP/field.json"
expect_fail "$TMP/field.json" "lacks required field .artifacts.apk.sha256"

# --- recipe hash mismatch -----------------------------------------------
echo "== recipe hash mismatch =="
jq '.recipe.committed_sha256["openwrt/Makefile"] = "0000000000000000000000000000000000000000000000000000000000000000"' \
  "$OUT/build-manifest-A.json" > "$TMP/recipe-mismatch.json"
expect_fail "$TMP/recipe-mismatch.json" "committed sha256"

# --- recipe file missing / extra ----------------------------------------
echo "== recipe file sets must match exactly =="
jq 'del(.recipe.committed_sha256["openwrt/Makefile"])' "$OUT/build-manifest-A.json" > "$TMP/recipe-missing.json"
expect_fail "$TMP/recipe-missing.json" "does not hash these committed recipe files"
jq '.recipe.committed_sha256["openwrt/NOTES.md"] = "0000000000000000000000000000000000000000000000000000000000000000"' \
  "$OUT/build-manifest-A.json" > "$TMP/recipe-extra.json"
expect_fail "$TMP/recipe-extra.json" "hashes files that are not in the recipe"

# --- overlay mismatch (prefix of the real hash) --------------------------
echo "== overlay hash mismatch =="
jq '.recipe.overlay.sha256 = "0000000000000000000000000000000000000000000000000000000000000000"' \
  "$OUT/build-manifest-A.json" > "$TMP/overlay.json"
expect_fail "$TMP/overlay.json" "overlay"

# --- replay mismatch: a recipe that cannot produce the built Makefile ----
echo "== unreplayable recipe =="
jq '.recipe.makefile_final_sha256 = "1111111111111111111111111111111111111111111111111111111111111111"' \
  "$OUT/build-manifest-A.json" > "$TMP/replay.json"
expect_fail "$TMP/replay.json" "the committed recipe cannot produce"

# --- wrong Go -----------------------------------------------------------
echo "== wrong Go version and wrong toolchain hash =="
jq '.build.go.version = "1.27.1"' "$OUT/build-manifest-A.json" > "$TMP/wrong-go.json"
expect_fail "$TMP/wrong-go.json" "below MIN_GO_VERSION"
jq '.build.go.tarball_sha256 = "2222222222222222222222222222222222222222222222222222222222222222"' \
  "$OUT/build-manifest-A.json" > "$TMP/wrong-go-tar.json"
expect_fail "$TMP/wrong-go-tar.json" "!= pinned GO_TOOLCHAIN_TARBALL_SHA256"

# --- wrong SDK digest ---------------------------------------------------
echo "== wrong SDK digest (script/workflow drift) =="
jq '.build.sdk_image = "openwrt/sdk@sha256:3333333333333333333333333333333333333333333333333333333333333333"' \
  "$OUT/build-manifest-A.json" > "$TMP/wrong-sdk.json"
expect_fail "$TMP/wrong-sdk.json" "!= the workflow pin"

# --- artifact mismatches ------------------------------------------------
echo "== artifact hash, name and version mismatches =="
jq '.artifacts.apk.sha256 = "4444444444444444444444444444444444444444444444444444444444444444"' \
  "$OUT/build-manifest-A.json" > "$TMP/apk-sha.json"
expect_fail "$TMP/apk-sha.json" "apk sha256" --apk "$OUT/goshisen-0.2.9-test-r1-A.apk"
jq '.artifacts.apk.name = "goshisen-0.2.9-r1.apk"' "$OUT/build-manifest-A.json" > "$TMP/apk-name.json"
expect_fail "$TMP/apk-name.json" "artifacts.apk.name"
jq '.artifacts.payload.sha256 = "5555555555555555555555555555555555555555555555555555555555555555"' \
  "$OUT/build-manifest-A.json" > "$TMP/pay-sha.json"
expect_fail "$TMP/pay-sha.json" "payload sha256" --payload "$OUT/daemon-A.bin"
jq 'del(.artifacts.apk.signed)' "$OUT/build-manifest-A.json" > "$TMP/no-sign-state.json"
expect_fail "$TMP/no-sign-state.json" "artifacts.apk.signed must be true or false" \
  --apk "$OUT/goshisen-0.2.9-test-r1-A.apk"

# --- signature-state re-check via APK_TOOL (stubbed: no real apk-tools here) --
echo "== APK_TOOL signature-state branch =="
printf '#!/bin/sh\necho "UNTRUSTED signature" >&2\nexit 1\n' > "$TMP/apk-unsigned"
printf '#!/bin/sh\necho "OK"\nexit 0\n' > "$TMP/apk-signed"
chmod +x "$TMP/apk-unsigned" "$TMP/apk-signed"
APK_TOOL="$TMP/apk-unsigned" "$CHECK" --manifest "$OUT/build-manifest-A.json" \
  --apk "$OUT/goshisen-0.2.9-test-r1-A.apk" 2>&1 | grep -q "UNTRUSTED signature (unsigned, as recorded)" \
  || fail "unsigned manifest + UNTRUSTED apk verify should PASS"
echo "  ok: unsigned manifest + UNTRUSTED apk verify passes"
jq '.artifacts.apk.signed = true' "$OUT/build-manifest-A.json" > "$TMP/claims-signed.json"
rc=0
out="$(APK_TOOL="$TMP/apk-unsigned" "$CHECK" --manifest "$TMP/claims-signed.json" \
  --apk "$OUT/goshisen-0.2.9-test-r1-A.apk" 2>&1)" || rc=$?
[ "$rc" -ne 0 ] || fail "a manifest claiming signed=true was ACCEPTED with an UNTRUSTED apk"
grep -qF "manifest says signed, but apk verify failed" <<<"$out" \
  || fail "signed-claim case failed for the wrong reason: $out"
echo "  ok: signed claim with an UNTRUSTED apk is rejected"
APK_TOOL="$TMP/apk-signed" "$CHECK" --manifest "$TMP/claims-signed.json" \
  --apk "$OUT/goshisen-0.2.9-test-r1-A.apk" 2>&1 | grep -q "apk verify: OK (signed, as recorded)" \
  || fail "signed manifest + OK apk verify should PASS"
echo "  ok: signed manifest + OK apk verify passes"
jq '.artifacts.apk.signed = true' "$OUT/build-manifest-A.json" > "$TMP/claims-signed2.json"
APK_TOOL="$TMP/apk-signed" "$CHECK" --manifest "$TMP/claims-signed2.json" --apk "$OUT/goshisen-0.2.9-test-r1-A.apk" >/dev/null \
  || fail "signed manifest with a signed stub must PASS"
echo "  ok: both signature-state directions covered (stubbed apk verify)"

# --- A/B compare --------------------------------------------------------
echo "== A/B comparison =="
jq '.label = "B" | .artifacts.apk.sha256 = "6666666666666666666666666666666666666666666666666666666666666666"' \
  "$OUT/build-manifest-A.json" > "$TMP/manifest-B.json"
expect_fail "$OUT/build-manifest-A.json" "A/B mismatch on .artifacts.apk.sha256" --compare "$TMP/manifest-B.json"
jq '.label = "A"' "$OUT/build-manifest-A.json" > "$TMP/same-label.json"
expect_fail "$OUT/build-manifest-A.json" "not two independent builds" --compare "$TMP/same-label.json"

# --- pin-gate integration (step 9) --------------------------------------
echo "== pin gate step 9 (scratch repo with a tag) =="
SCRATCH="$TMP/scratch"
mkdir -p "$SCRATCH"
git -C "$SCRATCH" init -q -b main
git -C "$SCRATCH" config user.email t@example.com
git -C "$SCRATCH" config user.name "fixture"
git -C "$SCRATCH" config commit.gpgsign false
mkdir -p "$SCRATCH/cmd/goshisen" "$SCRATCH/internal/app" "$SCRATCH/openwrt/files/etc/goshisen" \
         "$SCRATCH/packaging/apk-build"
printf 'module github.com/88kbleirad/GoShiSen-Enforce\n\ngo 1.27.4\n' > "$SCRATCH/go.mod"
printf 'package main\n' > "$SCRATCH/cmd/goshisen/main.go"
printf 'package app\n' > "$SCRATCH/internal/app/app.go"
printf 'x\n' > "$SCRATCH/openwrt/files/etc/goshisen/policy.yaml"
sed -E 's/^PKG_VERSION:=.*/PKG_VERSION:=0.2.9/; s/^PKG_RELEASE:=.*/PKG_RELEASE:=1/' \
  "$ROOT/openwrt/Makefile" > "$SCRATCH/openwrt/Makefile"
cp "$ROOT/packaging/apk-build/no-golang-host.patch" "$SCRATCH/packaging/apk-build/"
cp "$ROOT/packaging/apk-build/sdk-x86_64.config" "$SCRATCH/packaging/apk-build/"
git -C "$SCRATCH" add -A && git -C "$SCRATCH" commit -qm "fixture runtime"
PIN="$(git -C "$SCRATCH" rev-parse HEAD)"
sed -E -i "s|^PKG_SOURCE_VERSION:=.*|PKG_SOURCE_VERSION:=$PIN|" "$SCRATCH/openwrt/Makefile"
git -C "$SCRATCH" add -A && git -C "$SCRATCH" commit -qm "fixture recipe (pin)"
TIP="$(git -C "$SCRATCH" rev-parse HEAD)"
git -C "$SCRATCH" tag v0.2.9 "$TIP"

SOUT="$TMP/scratch-out"
gen_manifest "$SCRATCH" "$TIP" "0.2.9" "$SOUT"
# The pin gate (step 5) requires the release-canonical basename.
cp "$SOUT/goshisen-0.2.9-r1-A.apk" "$SOUT/goshisen-0.2.9-r1.apk"
GATE_REPO="$SCRATCH" bash "$GATE" \
  --apk "$SOUT/goshisen-0.2.9-r1.apk" --tag v0.2.9 \
  --payload "$SOUT/daemon-A.bin" --manifest "$SOUT/build-manifest-A.json" >"$TMP/gate.log" 2>&1 \
  || { tail -20 "$TMP/gate.log"; fail "pin gate with a valid manifest did not PASS"; }
grep -q "apk build manifest describes this commit" "$TMP/gate.log" \
  || fail "pin gate step 9 did not run"
grep -q "PASS: release pin/version agreement gate passed" "$TMP/gate.log" || fail "pin gate did not PASS"
echo "  ok: pin gate ran step 9 and PASSed"

jq '.build.sdk_image = "openwrt/sdk@sha256:7777777777777777777777777777777777777777777777777777777777777777"' \
  "$SOUT/build-manifest-A.json" > "$SOUT/bad.json"
rc=0
GATE_REPO="$SCRATCH" bash "$GATE" --apk "$SOUT/goshisen-0.2.9-r1.apk" --tag v0.2.9 \
  --payload "$SOUT/daemon-A.bin" --manifest "$SOUT/bad.json" >"$TMP/gate-bad.log" 2>&1 || rc=$?
[ "$rc" -ne 0 ] || fail "pin gate ACCEPTED a manifest with a drifted SDK digest"
grep -q "apk build manifest check failed" "$TMP/gate-bad.log" \
  || fail "pin gate failed for the wrong reason: $(tail -3 "$TMP/gate-bad.log")"
echo "  ok: pin gate fails closed on a bad manifest"

# --- the gate must stay opt-in (no manifest => no manifest check) --------
echo "== step 9 is opt-in =="
GATE_REPO="$SCRATCH" bash "$GATE" --apk "$SOUT/goshisen-0.2.9-r1.apk" --tag v0.2.9 \
  --payload "$SOUT/daemon-A.bin" >"$TMP/gate-nomanifest.log" 2>&1 \
  || { tail -10 "$TMP/gate-nomanifest.log"; fail "pin gate without --manifest did not PASS"; }
grep -q "apk build-manifest check skipped" "$TMP/gate-nomanifest.log" \
  || fail "pin gate did not announce the skipped manifest check"
echo "  ok: gate passes without a manifest but says so"

echo "APK-BUILD-MANIFEST-GATE-GUARD-PASS"