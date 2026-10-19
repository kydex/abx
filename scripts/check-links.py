#!/usr/bin/env python3
"""Check that every relative link in the Markdown files resolves.

Inline links, images, and reference definitions are followed. External URLs,
mail links, and bare anchors are skipped. File existence is checked; heading
anchors inside a target are not, because renderer slug rules differ and that
check stays manual.

The scan follows the CommonMark shape rather than the raw text: code spans and
backslash-escaped brackets are not links, a fenced block closes only with a
fence of the same character, not shorter than the opening one, and with
nothing after it, a destination may carry one balanced pair of
parentheses and an optional title, and a reference definition may put its
target on the next line. Indented code blocks are not recognized, so a
link-looking line inside one is scanned as text.
"""
import pathlib
import re
import sys
import urllib.parse

ROOT = pathlib.Path(__file__).resolve().parent.parent
LINK = re.compile(r"!?\[[^\]]*\]\(((?:[^()]|\([^()]*\))*)\)")
REFERENCE = re.compile(r"^\s*\[[^\]]+\]:\s*(\S+)")
REFERENCE_OPEN = re.compile(r"^\s*\[[^\]]+\]:\s*$")
FENCE = re.compile(r"^\s*(`{3,}|~{3,})")
CODE_SPAN = re.compile(r"(`+).*?\1")

SKIP = ("#", "http://", "https://", "mailto:", "tel:")


def targets(text):
    fence = None
    pending = False
    for number, line in enumerate(text.splitlines(), 1):
        match = FENCE.match(line)
        if match:
            run = match.group(1)
            if fence is None:
                fence = (run[0], len(run))
            elif run[0] == fence[0] and len(run) >= fence[1] and not line[match.end():].strip():
                fence = None
            pending = False
            continue
        if fence is not None:
            pending = False
            continue
        line = CODE_SPAN.sub(" ", line).replace("\\[", "")
        if pending:
            pending = False
            stripped = line.strip()
            if stripped:
                yield number, stripped.split()[0]
        for match in LINK.finditer(line):
            yield number, match.group(1)
        match = REFERENCE.match(line)
        if match:
            yield number, match.group(1)
        elif REFERENCE_OPEN.match(line):
            pending = True


def main():
    files = sorted([*ROOT.glob("*.md"), *ROOT.glob("docs/**/*.md")])
    checked = 0
    broken = []
    for path in files:
        text = path.read_text(encoding="utf-8")
        for number, raw in targets(text):
            target = raw.strip()
            if target.startswith("<") and ">" in target:
                target = target[1:target.index(">")]
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
