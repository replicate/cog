import asyncio
import os.path
import sys

from coglet import inspector, runner


async def main() -> int:
    if len(sys.argv) != 3:
        print(f'Usage {os.path.basename(sys.argv[0])} <MODULE> <CLASS>')
        sys.exit(1)

    p = inspector.create_predictor(sys.argv[1], sys.argv[2])
    r = runner.Runner(p)

    await r.setup()
    output = await r.test()
    print(f'Predictor passed test: {output}')

    return 0


if __name__ == '__main__':
    sys.exit(asyncio.run(main()))
