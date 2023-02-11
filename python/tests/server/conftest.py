import os
import threading
from typing import Any, Dict, Optional

from attrs import define
from fastapi.testclient import TestClient
import pytest

from cog.server.http import create_app


@define
class AppConfig:
    predictor_fixture: str
    options: Optional[Dict[str, Any]]


def _fixture_path(name):
    test_dir = os.path.dirname(os.path.realpath(__file__))
    return os.path.join(test_dir, f"fixtures/{name}.py") + ":Predictor"


def uses_predictor(name):
    return pytest.mark.parametrize(
        "client", [AppConfig(predictor_fixture=name, options={})], indirect=True
    )


def uses_predictor_with_client_options(name, **options):
    return pytest.mark.parametrize(
        "client", [AppConfig(predictor_fixture=name, options=options)], indirect=True
    )


def make_client(fixture_name: str, upload_url: Optional[str] = None):
    """
    Creates a fastapi test client for an app that uses the requested Predictor.
    """
    predictor_ref = _fixture_path(fixture_name)
    app = create_app(
        predictor_ref=predictor_ref,
        shutdown_event=threading.Event(),
        upload_url=upload_url,
    )
    return TestClient(app)


@pytest.fixture
def client(request):
    fixture_name = request.param.predictor_fixture
    options = request.param.options

    # Use context manager to trigger setup/shutdown events.
    with make_client(fixture_name=fixture_name, **options) as c:
        yield c
