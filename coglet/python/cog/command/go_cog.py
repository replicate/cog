import os
import os.path
import platform
import sys
from pathlib import Path
from typing import List


def run(subcmd: str, args: List[str]) -> None:
    goos = platform.system().lower()
    if goos not in {'linux', 'darwin'}:
        print(f'Unsupported OS: {goos}')
        sys.exit(1)

    goarch = platform.machine().lower()
    if goarch == 'x86_64':
        goarch = 'amd64'
    elif goarch == 'aarch64':
        goarch = 'arm64'
    if goarch not in {'amd64', 'arm64'}:
        print(f'Unsupported architecture: {goarch}')
        sys.exit(1)

    # Binaries are bundled in python/cog
    cmd = f'cog-{goos}-{goarch}'
    exe = os.path.join(Path(__file__).parent.parent, cmd)
    args = [exe, subcmd] + args
    # Replicate Go logger logs to stdout in production mode
    # Use stderr instead to be consistent with legacy Cog
    env = os.environ.copy()
    if 'LOG_FILE' not in env:
        env['LOG_FILE'] = 'stderr'
    os.execve(exe, args, env)
