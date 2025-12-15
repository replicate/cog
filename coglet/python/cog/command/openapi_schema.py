import sys

from cog.command import go_cog

# Lightweight wrapper from `python3 -m cog.command.openapi_schema` to `cog schema`
if __name__ == '__main__':
    go_cog.run('schema', sys.argv[1:])
