#!/usr/bin/env python3
"""Check local Markdown link targets in the maintained repository documents."""
from pathlib import Path
import re
from urllib.parse import unquote, urlsplit

root = Path(__file__).resolve().parent.parent
documents = list(root.glob('*.md'))
documents += list((root / 'docs').rglob('*.md'))
documents += list((root / 'zero-beat-gui').rglob('*.md'))
documents += [root / name / 'README.md' for name in
              ('zero-beat-core', 'zero-beat-tui', 'zero-beat-site')]
errors = []
checked = 0
for document in documents:
    for target in re.findall(r'!?\[[^\]]*\]\(([^\s)]+)', document.read_text()):
        target = target.strip('<>')
        url = urlsplit(target)
        if url.scheme or url.netloc or not url.path or url.path.startswith('/'):
            continue
        checked += 1
        if not (document.parent / unquote(url.path)).exists():
            errors.append(f'{document.relative_to(root)}: {target}')
if errors:
    raise SystemExit('Missing local link targets:\n' + '\n'.join(errors))
print(f'{checked} local documentation link targets checked')
