import os
import sys
import threading
import time
from contextlib import ExitStack
from typing import Any, Dict, Optional, Sequence, Tuple
from unittest import mock

import pytest
from attrs import define
from fastapi.testclient import TestClient

from cog.command import ast_openapi_schema
from cog.config import Config
from cog.server.http import create_app
from cog.server.worker import make_worker


@define
class AppConfig:
    predictor_fixture: str
    options: Optional[Dict[str, Any]]


@define
class WorkerConfig:
    fixture_name: str
    is_async: bool = False
    setup: bool = True
    max_concurrency: int = 1
    min_python: Optional[Tuple[int, int]] = None


def pytest_make_parametrize_id(config, val):
    """
    Generates more readable IDs for parametrized tests that use AppConfig or
    WorkerConfig values.
    """
    if isinstance(val, AppConfig):
        return val.predictor_fixture
    elif isinstance(val, WorkerConfig):
        return val.fixture_name


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


def uses_trainer(name):
    # HACK: `name` can either be in the form "<name>.py:train" or just "<name>".
    if ":" not in name:
        name = f"{name}.py:train"
    options = {"additional_config": {"train": _fixture_path(name)}}

    return pytest.mark.parametrize(
        "client", [AppConfig(predictor_fixture=name, options=options)], indirect=True
    )


def uses_predictor_with_client_options(name, **options):
    return pytest.mark.parametrize(
        "client", [AppConfig(predictor_fixture=name, options=options)], indirect=True
    )


def uses_worker(
    name_or_names, setup=True, max_concurrency=1, min_python=None, is_async=False
):
    """
    Decorator for tests that require a Worker instance. `name_or_names` can be
    a single fixture name, or a sequence (list, tuple) of fixture names. If
    it's a sequence, the test will be run once for each worker.

    If `setup` is True (the default) setup will be run before the test runs.
    """
    if isinstance(name_or_names, (tuple, list)):
        values = (
            WorkerConfig(
                fixture_name=n,
                setup=setup,
                max_concurrency=max_concurrency,
                min_python=min_python,
                is_async=is_async,
            )
            for n in name_or_names
        )
    else:
        values = (
            WorkerConfig(
                fixture_name=name_or_names,
                setup=setup,
                max_concurrency=max_concurrency,
                min_python=min_python,
                is_async=is_async,
            ),
        )
    return uses_worker_configs(list(values))


def uses_worker_configs(values: Sequence[WorkerConfig]):
    """
    Decorator for tests that require a Worker instance.  The test will be
    run once for each worker.  `configs` is a sequence (list, tuple, generator)
    of WorkerConfig.
    """
    return pytest.mark.parametrize("worker", values, indirect=True)


def make_client(
    fixture_name: str,
    upload_url: Optional[str] = None,
    additional_config: Optional[dict] = None,
):
    """
    Creates a fastapi test client for an app that uses the requested Predictor.
    """

    config = {"predict": _fixture_path(fixture_name)}
    if additional_config:
        config.update(additional_config)

    app = create_app(
        cog_config=Config(config=config),
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
        c.ref = fixture_name
        yield c


@pytest.fixture
def static_schema(client) -> dict:
    ref = _fixture_path(client.ref)
    module_path = ref.split(":", 1)[0]
    return ast_openapi_schema.extract_file(module_path)


@pytest.fixture
def worker(request):
    ref = _fixture_path(request.param.fixture_name)
    if (
        request.param.min_python is not None
        and sys.version_info < request.param.min_python
    ):
        pytest.skip(
            f"Test requires python {request.param.min_python[0]}.{request.param.min_python[1]}"
        )
    w = make_worker(
        predictor_ref=ref,
        is_async=request.param.is_async,
        is_train=False,
        tee_output=False,
        max_concurrency=request.param.max_concurrency,
    )
    if request.param.setup:
        assert not w.setup().result().error
    try:
        yield w
    finally:
        w.shutdown()
