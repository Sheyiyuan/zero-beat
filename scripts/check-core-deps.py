#!/usr/bin/env python3
"""Enforce the portable core boundary on every supported Go target."""
import os
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parent.parent
for target in ('linux', 'darwin', 'windows', 'android'):
    env = dict(os.environ, GOOS=target, GOARCH='arm64', CGO_ENABLED='0', GOWORK='off')
    deps = subprocess.check_output([os.environ.get('GO', 'go'), 'list', '-deps', './...'],
                                   cwd=root / 'zero-beat-core', env=env, text=True)
    forbidden = [p for p in deps.splitlines() if any(s in p for s in
                  ('zero-beat-tui', 'bubbletea', 'lipgloss', 'ebitengine/oto', 'godbus/dbus'))]
    if forbidden:
        raise SystemExit(f'{target}: forbidden core dependencies: {forbidden}')
    subprocess.run([os.environ.get('GO', 'go'), 'build', './...'],
                   cwd=root / 'zero-beat-core', env=env, check=True)
    print(f'{target}: core dependency boundary and build passed')
