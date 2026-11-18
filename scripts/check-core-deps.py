#!/usr/bin/env python3
"""Enforce the portable core boundary on every supported Go target."""
import os
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parent.parent
for target in ('linux', 'darwin', 'windows', 'android'):
    env = dict(os.environ, GOOS=target, GOARCH='arm64', CGO_ENABLED='0', GOWORK='off')
    packages = subprocess.check_output([os.environ.get('GO', 'go'), 'list', './...'],
                                       cwd=root / 'zero-beat-core', env=env, text=True).splitlines()
    # sysmedia is an optional public Linux adapter. Portable services must never
    # depend on it; other targets compile its dependency-free platform stub.
    portable = [p for p in packages if not p.endswith('/sysmedia')]
    deps = subprocess.check_output([os.environ.get('GO', 'go'), 'list', '-deps', *portable],
                                   cwd=root / 'zero-beat-core', env=env, text=True)
    forbidden = [p for p in deps.splitlines() if any(s in p for s in
                  ('zero-beat-tui', 'bubbletea', 'lipgloss', 'ebitengine/oto', 'godbus/dbus', 'zero-beat-core/sysmedia'))]
    if forbidden:
        raise SystemExit(f'{target}: forbidden core dependencies: {forbidden}')
    if target != 'linux':
        adapter = subprocess.check_output([os.environ.get('GO', 'go'), 'list', '-deps', './sysmedia'],
                                          cwd=root / 'zero-beat-core', env=env, text=True)
        if 'godbus/dbus' in adapter:
            raise SystemExit(f'{target}: Linux D-Bus leaked into platform stub')
    subprocess.run([os.environ.get('GO', 'go'), 'build', './...'],
                   cwd=root / 'zero-beat-core', env=env, check=True)
    print(f'{target}: core dependency boundary and build passed')
