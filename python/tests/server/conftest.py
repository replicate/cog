import os

from fastapi.testclient import TestClient
import pytest

from cog.server.http import create_app


class match:
    def __init__(self, pattern):
        self.pattern = pattern

    def __eq__(self, other):
        if not isinstance(other, dict):
            return self.pattern == other
        minimal = {k: other[k] for k in self.pattern.keys() if k in other}
        return self.pattern == minimal

    def __repr__(self):
        return f"match({repr(self.pattern)})"


def _fixture_path(name):
    test_dir = os.path.dirname(os.path.realpath(__file__))
    return os.path.join(test_dir, f"fixtures/{name}.py") + ":Predictor"


def uses_predictor(name):
    return pytest.mark.parametrize("client", [name], indirect=True)


def make_client(fixture_name: str):
    """
    Creates a fastapi test client for an app that uses the requested Predictor.
    """
    predictor_ref = _fixture_path(fixture_name)
    app = create_app(predictor_ref)
    return TestClient(app)


@pytest.fixture
def client(request):
    # Use context manager to trigger setup/shutdown events.
    with make_client(request.param) as c:
        yield c
