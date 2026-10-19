#!/usr/bin/env python3
"""Check that every relative link in the Markdown files resolves.

Inline links, images, and reference definitions are followed. External URLs,
mail links, and bare anchors are skipped. File existence is checked; heading
anchors inside a target are not, because renderer slug rules differ and that
check stays manual.
"""
import pathlib
import re
import sys
import urllib.parse

ROOT = pathlib.Path(__file__).resolve().parent.parent
LINK = re.compile(r"!?\[[^\]]*\]\(([^)]+)\)")
REFERENCE = re.compile(r"^\s*\[[^\]]+\]:\s*(\S+)")
FENCE = re.compile(r"^\s*(```|~~~)")

SKIP = ("#", "http://", "https://", "mailto:", "tel:")


def targets(text):
    in_fence = False
    for number, line in enumerate(text.splitlines(), 1):
        if FENCE.match(line):
            in_fence = not in_fence
            continue
        if in_fence:
            continue
        for match in LINK.finditer(line):
            yield number, match.group(1)
        match = REFERENCE.match(line)
        if match:
            yield number, match.group(1)


def main():
    files = sorted([*ROOT.glob("*.md"), *ROOT.glob("docs/**/*.md")])
    checked = 0
    broken = []
    for path in files:
        text = path.read_text(encoding="utf-8")
        for number, raw in targets(text):
            target = raw.strip()
            if target.startswith("<") and target.endswith(">"):
                target = target[1:-1]
            else:
                target = target.split()[0]
            if not target or target.startswith(SKIP):
                continue
            target = target.split("#", 1)[0]
            if not target:
                continue
            checked += 1
            resolved = (path.parent / urllib.parse.unquote(target)).resolve()
            if not resolved.exists():
                broken.append(f"{path.relative_to(ROOT)}:{number}: {raw}")
    if broken:
        print("broken relative links:", file=sys.stderr)
        for item in broken:
            print("  " + item, file=sys.stderr)
        return 1
    print(f"links ok: {checked} relative targets in {len(files)} files")
    return 0


if __name__ == "__main__":
    sys.exit(main())
