from cog import Input
from tests.util import check_python_version

check_python_version(min_version=(3, 10))

ERROR = 'error-prone usage of default=None'


def run(x: str = Input(default=None)) -> str:
    pass
