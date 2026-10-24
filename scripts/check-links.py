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
parentheses and an optional title, backslash-escaped parentheses in a
destination are unescaped, and a reference definition may put its target on
the next line. Indented code blocks are not recognized, so a
link-looking line inside one is scanned as text. The subset stops there:
nested brackets in link text, destinations split across lines, fences inside
block quotes or list items, and code spans split across lines are not modelled.
"""
import pathlib
import re
import sys
import urllib.parse

ROOT = pathlib.Path(__file__).resolve().parent.parent
LINK = re.compile(r"!?\[[^\]]*\]\(((?:[^()]|\([^()]*\))*)\)")
REFERENCE = re.compile(r"^\s*\[[^\]]+\]:\s*(<[^>\n]*>|\S+)")
REFERENCE_OPEN = re.compile(r"^\s*\[[^\]]+\]:\s*$")
FENCE = re.compile(r"^ {0,3}(`{3,}|~{3,})")
CODE_SPAN = re.compile(r"(`+).*?\1")

SKIP = ("#", "http://", "https://", "mailto:", "tel:")


def unescape_destination(value):
    """Apply CommonMark backslash escapes to parentheses in a destination."""
    return re.sub(r"\\([()])", r"\1", value)


def link_target(raw):
    """Return the destination part of a link or reference definition."""
    raw = raw.strip()
    if raw.startswith("<") and ">" in raw:
        return unescape_destination(raw[1:raw.index(">")])
    return unescape_destination(raw.split()[0]) if raw.split() else ""


def mask_escaped_brackets(line):
    """Hide brackets escaped by an odd backslash run from the link scanner."""
    out = []
    i = 0
    while i < len(line):
        if line[i] != "\\":
            out.append(line[i])
            i += 1
            continue
        j = i
        while j < len(line) and line[j] == "\\":
            j += 1
        count = j - i
        if j < len(line) and line[j] in "[]":
            out.append("\\" * (count // 2))
            out.append(" " if count % 2 else line[j])
            i = j + 1
        else:
            out.append("\\" * count)
            i = j
    return "".join(out)


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
        line = mask_escaped_brackets(CODE_SPAN.sub(" ", line))
        if pending:
            pending = False
            stripped = line.strip()
            if stripped:
                yield number, link_target(stripped)
        for match in LINK.finditer(line):
            yield number, match.group(1)
        match = REFERENCE.match(line)
        if match:
            yield number, match.group(1)
        elif REFERENCE_OPEN.match(line):
            pending = True


def main():
    if len(sys.argv) > 2:
        print("usage: check-links.py [root]", file=sys.stderr)
        return 2
    root = pathlib.Path(sys.argv[1]).resolve() if len(sys.argv) == 2 else ROOT
    files = sorted([*root.glob("*.md"), *root.glob("docs/**/*.md")])
    checked = 0
    broken = []
    for path in files:
        text = path.read_text(encoding="utf-8")
        for number, raw in targets(text):
            target = link_target(raw)
            if not target or target.startswith(SKIP):
                continue
            target = target.split("#", 1)[0]
            if not target:
                continue
            checked += 1
            resolved = (path.parent / urllib.parse.unquote(target)).resolve()
            if not resolved.exists():
                broken.append(f"{path.relative_to(root)}:{number}: {raw}")
    if broken:
        print("broken relative links:", file=sys.stderr)
        for item in broken:
            print("  " + item, file=sys.stderr)
        return 1
    if checked == 0:
        print("error: no relative links found; the scanner or input tree is wrong", file=sys.stderr)
        return 1
    print(f"links ok: {checked} relative targets in {len(files)} files")
    return 0


if __name__ == "__main__":
    sys.exit(main())
