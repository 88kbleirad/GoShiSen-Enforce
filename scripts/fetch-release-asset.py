#!/usr/bin/env python3
"""Fetch a draft-release asset from THIS repository.

Used by the apk-sign workflow (production mode) inside the OpenWrt SDK
container, which has python3 but no gh/jq/curl and whose apt repos are
stale.  Only this repository's releases are ever queried; the API host
is fixed to api.github.com by construction.

Usage: fetch-release-asset.py <tag> <asset> <output-file>
Exit 1 on any failure (fail-closed).
"""

import json
import os
import sys
import urllib.request

BASE = "https://api.github.com/repos/" + os.environ["GITHUB_REPOSITORY"]
TOKEN = os.environ["GITHUB_TOKEN"]


def api(path):
    req = urllib.request.Request(
        BASE + path,
        headers={"Authorization": "Bearer " + TOKEN, "Accept": "application/vnd.github+json"},
    )
    with urllib.request.urlopen(req) as r:
        return json.load(r)


def main():
    tag, asset, out = sys.argv[1], sys.argv[2], sys.argv[3]

    rid = None
    for rel in api("/releases?per_page=100"):
        if rel.get("draft") and rel.get("tag_name") == tag:
            rid = rel["id"]
            break
    if rid is None:
        sys.exit("FAIL: no draft release for " + tag)

    aid = None
    for a in api(f"/releases/{rid}/assets?per_page=100"):
        if a["name"] == asset:
            aid = a["id"]
            break
    if aid is None:
        sys.exit("FAIL: asset " + asset + " not found in draft release")

    req = urllib.request.Request(
        f"{BASE}/releases/assets/{aid}",
        headers={
            "Authorization": "Bearer " + TOKEN,
            "Accept": "application/octet-stream",
        },
    )
    with urllib.request.urlopen(req) as r, open(out, "wb") as f:
        f.write(r.read())


if __name__ == "__main__":
    main()
