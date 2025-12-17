import argparse
import asyncio
import importlib
import json
import logging
import os
import os.path
import sys
import time
from typing import Optional

from coglet import file_runner, scope


def pre_setup(logger: logging.Logger, working_dir: str) -> Optional[file_runner.Config]:
    if os.environ.get('R8_TORCH_VERSION', '') != '':
        logger.info('eagerly importing torch')
        importlib.import_module('torch')

    # Cog server waits until user files become available and passes config to Python runner
    conf_file = os.path.join(working_dir, 'config.json')
    elapsed = 0.0
    timeout = 60.0
    while elapsed < timeout:
        if os.path.exists(conf_file):
            logger.info(f'config file found after {elapsed:.2f}s: {conf_file}')
            with open(conf_file, 'r') as f:
                conf = json.load(f)
                os.unlink(conf_file)
            config = file_runner.Config(
                module_name=conf['module_name'],
                predictor_name=conf['predictor_name'],
                max_concurrency=conf['max_concurrency'],
            )

            # Add user venv to PYTHONPATH
            pv = f'python{sys.version_info.major}.{sys.version_info.minor}'
            venv = os.path.join('/', 'root', '.venv', 'lib', pv, 'site-packages')
            if venv is not None and venv not in sys.path and os.path.exists(venv):
                logger.info(f'adding venv to PYTHONPATH: {venv}')
                sys.path.append(venv)
                # In case the model forks Python interpreter
                os.environ['PYTHONPATH'] = ':'.join(sys.path)

            return config
        time.sleep(0.01)
        elapsed += 0.01

    logger.error(f'config file not found after {timeout:.2f}s: {conf_file}')
    return None


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument('--name', metavar='NAME', required=True, help='name')
    parser.add_argument('--ipc-url', metavar='URL', required=True, help='IPC URL')
    parser.add_argument(
        '--working-dir', metavar='DIR', required=True, help='working directory'
    )

    logger = logging.getLogger('coglet')
    logger.setLevel(os.environ.get('COG_LOG_LEVEL', 'INFO').upper())
    handler = logging.StreamHandler(sys.stderr)
    handler.setFormatter(
        logging.Formatter(
            '%(asctime)s\t%(levelname)s\t[%(name)s]\t%(filename)s:%(lineno)d\t%(message)s'
        )
    )
    logger.addHandler(handler)

    _stdout_write = sys.stdout.write
    _stderr_write = sys.stderr.write

    sys.stdout.write = scope.ctx_write(_stdout_write)  # type: ignore
    sys.stderr.write = scope.ctx_write(_stderr_write)  # type: ignore

    args = parser.parse_args()

    config = pre_setup(logger, args.working_dir)
    if config is None:
        return -1

    return asyncio.run(
        file_runner.FileRunner(
            logger=logger,
            name=args.name,
            ipc_url=args.ipc_url,
            working_dir=args.working_dir,
            config=config,
        ).start()
    )


if __name__ == '__main__':
    sys.exit(main())
