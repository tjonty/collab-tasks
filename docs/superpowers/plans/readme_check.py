"""Checks README.md for the README update plan. Local only; never committed.

Run from the repo root: python3 docs/superpowers/plans/readme_check.py
Prints one line per problem and exits 1, or prints "README OK" and exits 0.
"""
import os
import re
import sys

ORDER = [
    "Quick start", "Try it", "Architecture", "Data flow and synchronization",
    "Technology choices", "Decisions and tradeoffs", "Assumptions", "Scaling",
    "Extended challenges", "Tests", "Not included and next steps", "Code map",
]
IMAGES = ["docs/images/system-map.png", "docs/images/life-of-a-change.png"]
NUMBERS = ["482.7", "899.1", "89.0", "89,400", "502.0", "393,233", "2,586,735"]

readme = open("README.md", encoding="utf-8").read()
perf = open("docs/performance.md", encoding="utf-8").read()

# Headings and links outside fenced code blocks only.
prose = re.sub(r"^```.*?^```", "", readme, flags=re.S | re.M)
headings = re.findall(r"^(#{1,6}) +(.+?) *$", prose, flags=re.M)
problems = []


def slug(text):
    """GitHub's anchor for a heading: lowercase, punctuation other than - and spaces removed, spaces to -."""
    text = re.sub(r"[`*_]", "", text).lower()
    return re.sub(r"[^\w\- ]", "", text).replace(" ", "-")


# order: the H2 headings, exactly and in order.
h2 = [t for level, t in headings if level == "##"]
if h2 != ORDER:
    for name in ORDER:
        if name not in h2:
            problems.append(f"order: missing ## {name}")
    for name in h2:
        if name not in ORDER:
            problems.append(f"order: unexpected ## {name}")
    present = [n for n in h2 if n in ORDER]
    if present != [n for n in ORDER if n in present]:
        problems.append(f"order: wrong order: {' / '.join(present)}")

# anchors: every in-page link matches some heading's slug.
slugs = {slug(t) for _, t in headings}
for target in re.findall(r"\]\(#([^)]+)\)", prose):
    if target not in slugs:
        problems.append(f"anchors: #{target} matches no heading")

# files: every relative link target exists.
for target in re.findall(r"\]\(([^)]+)\)", prose):
    if target.startswith(("http://", "https://", "#", "mailto:")):
        continue
    path = target.split("#", 1)[0]
    if path and not os.path.exists(path):
        problems.append(f"files: {path} doesn't exist")

# images: both diagrams are referenced and exist.
for image in IMAGES:
    if f"]({image})" not in prose:
        problems.append(f"images: {image} isn't referenced")
    if not os.path.isfile(image):
        problems.append(f"images: {image} doesn't exist")

# numbers: the headline results match docs/performance.md.
for n in NUMBERS:
    for name, text in (("README.md", readme), ("docs/performance.md", perf)):
        if n not in text:
            problems.append(f"numbers: {n} isn't in {name}")

print("\n".join(problems) if problems else "README OK")
sys.exit(1 if problems else 0)
