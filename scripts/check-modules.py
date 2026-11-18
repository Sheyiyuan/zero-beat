#!/usr/bin/env python3
"""Build unpublished sibling modules through an ephemeral Go module proxy."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parent.parent
MODULES = ('zero-beat-core', 'zero-beat-tui', 'zero-beat-gui')
PREFIX = 'github.com/sheyiyuan/zero-beat-'


def make_proxy(destination):
    version = re.search(r'github.com/sheyiyuan/zero-beat-core (v\S+)',
                        (ROOT / 'zero-beat-tui/go.mod').read_text())[1]
    for name in MODULES:
        source = ROOT / name
        module = 'github.com/sheyiyuan/' + name
        output = destination / module / '@v'
        output.mkdir(parents=True, exist_ok=True)
        (output / 'list').write_text(version + '\n')
        (output / (version + '.mod')).write_bytes((source / 'go.mod').read_bytes())
        (output / (version + '.info')).write_text(json.dumps(
            {'Version': version, 'Time': '2026-11-16T00:00:00Z'}))
        with zipfile.ZipFile(output / (version + '.zip'), 'w', zipfile.ZIP_DEFLATED) as archive:
            for path in sorted(source.rglob('*')):
                if not path.is_file() or path.is_symlink():
                    continue
                rel = path.relative_to(source)
                if any(p.startswith('.') or p in {'build', 'bin', 'dist', 'node_modules'} for p in rel.parts) or path.suffix in ('.exe', '.test'):
                    continue
                if name == 'zero-beat-gui' and rel.parts[0] in ('app', 'design'):
                    continue
                archive.write(path, f'{module}@{version}/{rel.as_posix()}')
            if not (source / 'LICENSE').exists():
                archive.write(ROOT / 'LICENSE', f'{module}@{version}/LICENSE')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--tidy', action='store_true')
    parser.add_argument('--compile-only', action='store_true', help='check isolated compilation/vet; do not execute tests')
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix='zero-modules-') as temp:
        temp = Path(temp)
        proxy = temp / 'proxy'
        make_proxy(proxy)
        env = os.environ.copy()
        upstream = env.get('GOPROXY', 'https://proxy.golang.org,direct')
        env.update(GOWORK='off', GOPROXY=proxy.as_uri() + ',' + upstream,
                   GONOSUMDB=','.join(filter(None, (env.get('GONOSUMDB', ''), PREFIX + '*'))), ZERO_TEST_PROXY=proxy.as_uri() + ',' + upstream)
        env['GOMODCACHE'] = str(temp / 'modcache')
        # Temporary copies are source archives, not Git checkouts.
        env['GOFLAGS'] = (env.get('GOFLAGS', '') + ' -buildvcs=false').strip()
        go = env.get('GO', 'go')
        for name in MODULES:
            module_dir = ROOT / name
            if args.tidy:
                checkout = temp / name
                shutil.copytree(module_dir, checkout, ignore=shutil.ignore_patterns('build', 'bin', 'dist', 'node_modules', '.dart_tool', '.gradle', '.git'))
                sums = checkout / 'go.sum'
                if sums.exists():
                    sums.write_text(''.join(line for line in sums.read_text().splitlines(True)
                                            if not line.startswith(PREFIX)))
                subprocess.run([go, 'mod', 'tidy'], cwd=checkout, env=env, check=True)
                sums.write_text(''.join(line for line in sums.read_text().splitlines(True)
                                        if not line.startswith(PREFIX)))
                shutil.copyfile(checkout / 'go.mod', module_dir / 'go.mod')
                shutil.copyfile(sums, module_dir / 'go.sum')
                # The TUI must see the just-tidied core manifest in this run.
                if name == 'zero-beat-core':
                    make_proxy(proxy)
            else:
                checkout = temp / name
                shutil.copytree(module_dir, checkout, ignore=shutil.ignore_patterns('build', 'bin', 'dist', 'node_modules', '.dart_tool', '.gradle', '.git'))
                # This proxy contains the current, unpublished sibling sources.
                # Discard only their old local hashes in the temporary checkout;
                # third-party checksum verification stays intact.
                sums = checkout / 'go.sum'
                if sums.exists():
                    sums.write_text(''.join(line for line in sums.read_text().splitlines(True)
                                            if not line.startswith(PREFIX)))
                test_command = ['test', '-mod=mod'] + (['-run', '^$'] if args.compile_only else []) + ['./...']
                for command in (test_command, ['vet', '-mod=mod', './...'],
                                ['build', '-mod=mod', './...']):
                    subprocess.run([go, *command], cwd=checkout, env=env, check=True)
                if (checkout / 'go.mod').read_bytes() != (module_dir / 'go.mod').read_bytes():
                    raise SystemExit(f'{name}: isolated build changed go.mod; run make tidy')


if __name__ == '__main__':
    main()
