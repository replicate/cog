import os
import threading
import time
from contextlib import ExitStack
from typing import Any, Dict, Optional
from unittest import mock

import pytest
from attrs import define
from cog.server.http import create_app
from fastapi.testclient import TestClient


@define
class AppConfig:
    predictor_fixture: str
    options: Optional[Dict[str, Any]]


def _fixture_path(name):
    # HACK: `name` can either be in the form "<name>.py:Predictor" or just "<name>".
    if ":" not in name:
        name = f"{name}.py:Predictor"

    test_dir = os.path.dirname(os.path.realpath(__file__))
    return os.path.join(test_dir, f"fixtures/{name}")


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
    config = {"predict": _fixture_path(fixture_name)}
    app = create_app(
        config=config,
        shutdown_event=threading.Event(),
        upload_url=upload_url,
    )
    return TestClient(app)


def wait_for_setup(client: TestClient):
    while True:
        resp = client.get("/health-check")
        data = resp.json()
        if data["status"] != "STARTING":
            break
        time.sleep(0.01)


@pytest.fixture
def client(request):
    fixture_name = request.param.predictor_fixture
    options = request.param.options

    with ExitStack() as stack:
        if "env" in options:
            stack.enter_context(mock.patch.dict(os.environ, options["env"]))
            del options["env"]

        # Use context manager to trigger setup/shutdown events.
        c = make_client(fixture_name=fixture_name, **options)
        stack.enter_context(c)
        wait_for_setup(c)
        yield c
