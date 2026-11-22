#!/usr/bin/env python3
"""Structural F-02 guard for the release workflow (issue #100).

Goal of F-02: the CLI release signing key
(`secrets.GH_RELEASE_SIGNING_KEY`, production fingerprint
`24215e01...`, key id `f955ede984245d64`) must be usable only by the
`sign` job of `.github/workflows/release.yml`, which runs under the
protected `release-cli` environment (required reviewer, deploy policy:
main + v* tags). The build job must never see the key.

Rules enforced (each has a negative fixture in
tests/ci/release-workflow-structure-test.sh):

  sign-environment         `sign` declares `environment: release-cli`;
                           no other job declares an `environment`.
  sign-only-write          `sign` has `contents: write`; validate/build
                           do not (read-only release pipeline).
  secret-scope             referencing the secret anywhere outside the
                           `sign` job fails — in release.yml and in every
                           other scanned workflow (where it must then be
                           inside a job that declares `environment:
                           release-cli`).
  secret-not-in-shell      the secret must never be interpolated into a
                           `run:` body (env-only: no shell/argv/log path).
  build-readonly           `build` permissions are exactly
                           `contents: read`.
  no-github-context-in-run no `run:` body interpolates `${{ github.* }}`
                           (S-003 class: ref-name script injection).
  dry-run-signs-with-key   the dry-run path signs with the real key via
                           scripts/release-sign.sh under the environment.
  dry-run-no-writes        dry-run steps never run `gh release`, `git
                           tag`, `git push` or upload artifacts.
  release-step-guarded     every step running `gh release` is skipped
                           when `inputs.dry_run` is true.
  dry-run-main-only        a dry-run step fails closed on any ref other
                           than refs/heads/main.
  fingerprint-from-file    the expected CLI anchor fingerprint is read from
                           keys/release-anchor-fingerprint.txt, never
                           hardcoded in the workflow (rotation safety).

Usage:
  check-release-workflow-structure.py [--release PATH] [--scan PATH]...
  check-release-workflow-structure.py --print-step JOB NAME_PREFIX
  check-release-workflow-structure.py --list-steps JOB
"""
from __future__ import annotations

import argparse
import re
import sys

import yaml

SECRET_NAME = "GH_RELEASE_SIGNING_KEY"
SECRET_EXPR = "secrets.GH_RELEASE_SIGNING_KEY"
ENVIRONMENT = "release-cli"
SIGN_JOB = "sign"
RELEASE_WRITE_RE = re.compile(r"\bgh\s+release\b")
GITHUB_CTX_RE = re.compile(r"\$\{\{\s*github\.")
MAIN_GUARD_RE = re.compile(r"refs/heads/main")

violations: list[str] = []


def load(path: str) -> dict:
    try:
        with open(path, encoding="utf-8") as fh:
            doc = yaml.safe_load(fh)
    except Exception as exc:  # noqa: BLE001 — any failure must fail closed
        raise SystemExit(f"FAIL [parse] {path}: cannot parse YAML: {exc}")
    if not isinstance(doc, dict):
        raise SystemExit(f"FAIL [parse] {path}: not a YAML mapping")
    return doc


def jobs_of(doc: dict) -> dict:
    jobs = doc.get("jobs")
    if not isinstance(jobs, dict):
        raise SystemExit("FAIL [parse] workflow has no jobs mapping")
    return jobs


def walk_strings(obj, path=()):
    """Yield (path_tuple, string) for every string value in obj."""
    if isinstance(obj, dict):
        for key, val in obj.items():
            yield from walk_strings(val, path + (str(key),))
    elif isinstance(obj, list):
        for idx, val in enumerate(obj):
            yield from walk_strings(val, path + (str(idx),))
    elif isinstance(obj, str):
        yield (path, obj)


def steps_of(job: dict):
    steps = job.get("steps")
    return steps if isinstance(steps, list) else []


def run_body(step: dict) -> str:
    body = step.get("run")
    return body if isinstance(body, str) else ""


def step_env(step: dict) -> dict:
    env = step.get("env")
    return env if isinstance(env, dict) else {}


def dry_run_condition(step: dict) -> str:
    return str(step.get("if", ""))


def is_dry_run_guarded(step: dict) -> bool:
    """True only for a step that RUNS during the dry run (truthy guard)."""
    cond = dry_run_condition(step).replace(" ", "")
    return "inputs.dry_run" in cond and "!inputs.dry_run" not in cond


def add(rule: str, msg: str) -> None:
    violations.append(f"FAIL [{rule}] {msg}")


# --------------------------------------------------------------------------
# rule implementations: each appends violations


def rule_sign_environment(wf: dict) -> None:
    jobs = jobs_of(wf)
    if SIGN_JOB not in jobs:
        add("sign-environment", "no `sign` job in release.yml")
        return
    sign_env = jobs[SIGN_JOB].get("environment")
    if sign_env != ENVIRONMENT:
        add("sign-environment", f"`sign` job must declare environment: {ENVIRONMENT}, got {sign_env!r}")
    for name, job in jobs.items():
        if name == SIGN_JOB:
            continue
        if "environment" in job:
            add("sign-environment", f"job {name!r} declares environment {job['environment']!r}; only `sign` may")


def rule_sign_only_write(wf: dict) -> None:
    jobs = jobs_of(wf)
    for name, job in jobs.items():
        perms = job.get("permissions")
        writes = []
        if isinstance(perms, dict):
            writes = [f"{k}: {v}" for k, v in perms.items() if str(v) != "read"]
        elif isinstance(perms, str) and perms != "read-all":
            writes = [perms]
        if name == SIGN_JOB:
            if not isinstance(perms, dict) or perms.get("contents") != "write":
                add("sign-only-write", f"`sign` permissions must be contents: write, got {perms!r}")
        elif writes:
            add("sign-only-write", f"job {name!r} has write permissions {writes}; only `sign` may write")


def rule_secret_scope(wf: dict, others: list[tuple[str, dict]]) -> None:
    jobs = jobs_of(wf)
    for name, job in jobs.items():
        for path, val in walk_strings(job):
            if SECRET_NAME in val and name != SIGN_JOB:
                add("secret-scope", f"release.yml job {name!r} references the CLI key ({'.'.join(path)})")
    # Workflow-level (outside any job) references are never environment-scoped.
    top = {k: v for k, v in wf.items() if k != "jobs"}
    for path, val in walk_strings(top):
        if SECRET_NAME in val:
            add("secret-scope", f"release.yml references the CLI key outside any job ({'.'.join(path)})")
    for path, doc in others:
        for name, job in jobs_of(doc).items():
            for jpath, val in walk_strings(job):
                if SECRET_NAME in val and job.get("environment") != ENVIRONMENT:
                    add("secret-scope", f"{path} job {name!r} references the CLI key without environment: {ENVIRONMENT} ({'.'.join(jpath)})")
        for path2, val in walk_strings({k: v for k, v in doc.items() if k != "jobs"}):
            if SECRET_NAME in val:
                add("secret-scope", f"{path} references the CLI key outside any job ({'.'.join(path2)})")


def rule_secret_not_in_shell(wf: dict) -> None:
    for name, job in jobs_of(wf).items():
        for idx, step in enumerate(steps_of(job)):
            if SECRET_NAME in run_body(step):
                add("secret-not-in-shell", f"{name}.steps[{idx}] interpolates the CLI key into `run:`; use env:")


def rule_build_readonly(wf: dict) -> None:
    jobs = jobs_of(wf)
    build = jobs.get("build")
    if build is None:
        add("build-readonly", "no `build` job in release.yml")
        return
    perms = build.get("permissions")
    if not isinstance(perms, dict) or perms.get("contents") != "read" or len(perms) != 1:
        add("build-readonly", f"`build` permissions must be exactly contents: read, got {perms!r}")
    if build.get("environment") is not None:
        add("build-readonly", "`build` must not declare an environment")


def rule_no_github_context_in_run(wf: dict) -> None:
    for name, job in jobs_of(wf).items():
        for idx, step in enumerate(steps_of(job)):
            m = GITHUB_CTX_RE.search(run_body(step))
            if m:
                add("no-github-context-in-run", f"{name}.steps[{idx}] `run:` interpolates {m.group(0)}... (S-003); pass it via env")


def rule_dry_run_signs_with_key(wf: dict) -> None:
    jobs = jobs_of(wf)
    sign = jobs.get(SIGN_JOB)
    if sign is None:
        add("dry-run-signs-with-key", "no `sign` job")
        return
    found = False
    for step in steps_of(sign):
        if SECRET_EXPR in " ".join(str(v) for v in step_env(step).values()) \
                and "release-sign.sh" in run_body(step) \
                and is_dry_run_guarded(step):
            found = True
    if not found:
        add("dry-run-signs-with-key",
            "no dry-run step signs with the CLI key via scripts/release-sign.sh")


def rule_dry_run_no_writes(wf: dict) -> None:
    for name, job in jobs_of(wf).items():
        for idx, step in enumerate(steps_of(job)):
            if not is_dry_run_guarded(step):
                continue
            body = run_body(step)
            uses = str(step.get("uses", ""))
            for pat in ("gh release", "git push", "git tag", "upload-artifact"):
                if pat in body or pat in uses:
                    add("dry-run-no-writes", f"{name}.steps[{idx}] is a dry-run step but performs {pat!r}")


def rule_release_step_guarded(wf: dict) -> None:
    for name, job in jobs_of(wf).items():
        for idx, step in enumerate(steps_of(job)):
            if RELEASE_WRITE_RE.search(run_body(step)):
                cond = dry_run_condition(step)
                if "inputs.dry_run" not in cond or "!" not in cond:
                    add("release-step-guarded", f"{name}.steps[{idx}] runs `gh release` without `if: ${{{{ !inputs.dry_run }}}}`")


def rule_fingerprint_from_file(wf: dict) -> None:
    """Rotating the CLI key must be a data change, not a workflow edit."""
    hex64 = re.compile(r"\b[0-9a-f]{64}\b")
    for name, job in jobs_of(wf).items():
        for idx, step in enumerate(steps_of(job)):
            m = hex64.search(run_body(step))
            if m:
                add("fingerprint-from-file",
                    f"{name}.steps[{idx}] hardcodes a 64-hex literal "
                    f"({m.group(0)[:8]}...); read keys/release-anchor-fingerprint.txt instead")
    joined = "\n".join(run_body(st) for j in jobs_of(wf).values() for st in steps_of(j))
    if "release-anchor-fingerprint.txt" not in joined:
        add("fingerprint-from-file",
            "release.yml must read the expected fingerprint from keys/release-anchor-fingerprint.txt")
    if "keys/goshisen-release.pub.pem" not in joined:
        add("fingerprint-from-file",
            "release.yml must verify the committed anchor keys/goshisen-release.pub.pem")


def rule_dry_run_main_only(wf: dict) -> None:
    jobs = jobs_of(wf)
    sign = jobs.get(SIGN_JOB) or {}
    for step in steps_of(sign):
        if is_dry_run_guarded(step) \
                and MAIN_GUARD_RE.search(run_body(step)) \
                and re.search(r"\bexit 1\b", run_body(step)):
            return
    add("dry-run-main-only", "no dry-run step in `sign` rejects refs other than refs/heads/main")


# --------------------------------------------------------------------------
# CLI


def print_step(path: str, job: str, prefix: str) -> int:
    doc = load(path)
    jobs = jobs_of(doc)
    if job not in jobs:
        print(f"FAIL [print-step] no job {job!r}", file=sys.stderr)
        return 2
    for step in steps_of(jobs[job]):
        name = str(step.get("name", ""))
        if name.startswith(prefix) and run_body(step):
            sys.stdout.write(run_body(step))
            return 0
    print(f"FAIL [print-step] no run step named {prefix!r} in job {job!r}", file=sys.stderr)
    return 2


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--release", default=None)
    ap.add_argument("--scan", action="append", default=[], help="additional workflow files for the repo-wide secret rule")
    ap.add_argument("--print-step", nargs=2, metavar=("JOB", "NAME_PREFIX"))
    ap.add_argument("files", nargs="*", help="workflow files; the first is the release workflow unless --release is given")
    args = ap.parse_args()

    files = list(args.files)
    release = args.release
    if release is None:
        release = files.pop(0) if files else ".github/workflows/release.yml"

    if args.print_step:
        return print_step(release, args.print_step[0], args.print_step[1])

    wf = load(release)
    others = []
    for path in list(args.scan) + files:
        if path == args.release:
            continue
        others.append((path, load(path)))

    rule_sign_environment(wf)
    rule_sign_only_write(wf)
    rule_secret_scope(wf, others)
    rule_secret_not_in_shell(wf)
    rule_build_readonly(wf)
    rule_no_github_context_in_run(wf)
    rule_dry_run_signs_with_key(wf)
    rule_dry_run_no_writes(wf)
    rule_release_step_guarded(wf)
    rule_dry_run_main_only(wf)
    rule_fingerprint_from_file(wf)
    for other_path, other in others:
        for name, job in jobs_of(other).items():
            for idx, step in enumerate(steps_of(job)):
                if SECRET_NAME in run_body(step):
                    add("secret-not-in-shell", f"{other_path} job {name!r} step {idx} interpolates the CLI key into `run:`")

    if violations:
        for line in violations:
            print(line, file=sys.stderr)
        print(f"RELEASE-WORKFLOW-STRUCTURE-FAIL ({len(violations)} violation(s))", file=sys.stderr)
        return 1
    print("RELEASE-WORKFLOW-STRUCTURE-PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
