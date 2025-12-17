import sys

from cog.command import go_cog

# Lightweight wrapper from `python3 -m cog.command.test` to `cog test`
if __name__ == '__main__':
    go_cog.run('test', sys.argv[1:])
