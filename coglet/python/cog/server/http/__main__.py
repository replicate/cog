import os
import sys

from cog.command import go_cog

# Lightweight wrapper from `python3 -m cog.server.http` to `cog server`
if __name__ == '__main__':
    args = sys.argv[1:]
    port = os.getenv('PORT')
    if port is not None:
        args += ['--port', port]
    go_cog.run('server', args)
