# docs-site

Static documentation site for GoShiSen-Enforce. See the "Documentation
site" section in the repo README for usage.

## Regenerate the manifest

The sidebar is driven by `manifest.json`, generated from the repo's
markdown files. Regenerate after adding/renaming/removing docs:

```bash
python3 - <<'EOF'
import json, os, re, glob
files = [("root", p) for p in ["README.md","CONTRIBUTING.md","SECURITY.md"]]
files += [("docs", p) for p in sorted(glob.glob("docs/*.md"))]
files += [("design", p) for p in sorted(glob.glob("docs/design/*.md"))]
files += [("release", p) for p in sorted(glob.glob("docs/release/*.md"))]
def slug(p): return p.replace(".md","").replace("/","-").lower()
out = [{"slug": slug(p), "path": "../"+p, "title": open(p, encoding="utf-8").readline().lstrip("# ").strip()} for _,p in files]
json.dump(out, open("manifest.json","w"), indent=1, ensure_ascii=False)
EOF
```

## Adding a Vietnamese translation

Mirror the English file under `translations/vi/` with the same relative
path (e.g. `docs/user-guide.md` → `translations/vi/docs/user-guide.md`).
Preserve code blocks, tables, and inline code verbatim; translate only
prose. Missing translations fall back to English with a notice.
