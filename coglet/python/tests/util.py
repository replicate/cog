import sys
from typing import Optional, Tuple


def check_python_version(
    min_version: Optional[Tuple[int, int]] = None,
    max_version: Optional[Tuple[int, int]] = None,
) -> None:
    if min_version is not None and sys.version_info < min_version:
        raise PythonVersionError(
            f'Python version must be >= {min_version[0]}.{min_version[1]}'
        )
    if max_version is not None and sys.version_info > max_version:
        raise PythonVersionError(
            f'Python version must be <= {max_version[0]}.{max_version[1]}'
        )


class PythonVersionError(Exception):
    pass
