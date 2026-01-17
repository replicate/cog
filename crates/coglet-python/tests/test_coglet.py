"""Tests for coglet Python bindings."""

import json
import subprocess
import sys
import tempfile
import time
from pathlib import Path

import pytest
import requests


@pytest.fixture
def sync_predictor(tmp_path: Path) -> Path:
    """Create a simple sync predictor."""
    predictor = tmp_path / "predict.py"
    predictor.write_text("""
from cog import BasePredictor

class Predictor(BasePredictor):
    def setup(self):
        self.prefix = "Hello, "
    
    def predict(self, name: str = "World") -> str:
        return self.prefix + name + "!"
""")
    return predictor


@pytest.fixture
def generator_predictor(tmp_path: Path) -> Path:
    """Create a generator predictor."""
    predictor = tmp_path / "predict.py"
    predictor.write_text("""
from cog import BasePredictor
from typing import Iterator

class Predictor(BasePredictor):
    def setup(self):
        pass
    
    def predict(self, count: int = 3) -> Iterator[str]:
        for i in range(count):
            yield f"chunk {i}"
""")
    return predictor


@pytest.fixture
def async_predictor(tmp_path: Path) -> Path:
    """Create an async predictor."""
    predictor = tmp_path / "predict.py"
    predictor.write_text("""
import asyncio
from cog import BasePredictor

class Predictor(BasePredictor):
    def setup(self):
        self.call_count = 0
    
    async def predict(self, delay: float = 0.1, name: str = "test") -> str:
        self.call_count += 1
        await asyncio.sleep(delay)
        return f"{name}: done after {delay}s (call #{self.call_count})"
""")
    return predictor


@pytest.fixture
def async_generator_predictor(tmp_path: Path) -> Path:
    """Create an async generator predictor."""
    predictor = tmp_path / "predict.py"
    predictor.write_text("""
import asyncio
from cog import BasePredictor
from typing import AsyncIterator

class Predictor(BasePredictor):
    def setup(self):
        pass
    
    async def predict(self, count: int = 3, delay: float = 0.05) -> AsyncIterator[str]:
        for i in range(count):
            await asyncio.sleep(delay)
            yield f"async chunk {i}"
""")
    return predictor


class CogletServer:
    """Context manager for running coglet server."""

    def __init__(self, predictor_path: Path, port: int = 5599):
        self.predictor_path = predictor_path
        self.port = port
        self.process = None

    def __enter__(self):
        cmd = [
            sys.executable,
            "-c",
            f"import coglet; coglet.serve('{self.predictor_path}:Predictor', port={self.port})",
        ]
        self.process = subprocess.Popen(
            cmd,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        # Wait for server to start
        self._wait_for_ready()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        if self.process:
            self.process.terminate()
            self.process.wait(timeout=5)

    def _wait_for_ready(self, timeout: float = 10.0):
        start = time.time()
        while time.time() - start < timeout:
            try:
                resp = requests.get(
                    f"http://localhost:{self.port}/health-check", timeout=1
                )
                if resp.status_code == 200 and resp.json().get("status") == "READY":
                    return
            except requests.exceptions.ConnectionError:
                pass
            time.sleep(0.1)
        raise TimeoutError(f"Server did not become ready within {timeout}s")

    @property
    def base_url(self) -> str:
        return f"http://localhost:{self.port}"

    def health_check(self) -> dict:
        resp = requests.get(f"{self.base_url}/health-check")
        resp.raise_for_status()
        return resp.json()

    def predict(self, input_data: dict) -> dict:
        resp = requests.post(
            f"{self.base_url}/predictions",
            json={"input": input_data},
        )
        return resp.json()


class TestHealthCheck:
    """Tests for health check endpoint."""

    def test_returns_ready_status(self, sync_predictor: Path):
        with CogletServer(sync_predictor, port=5600) as server:
            health = server.health_check()
            assert health["status"] == "READY"

    def test_returns_version_info(self, sync_predictor: Path):
        with CogletServer(sync_predictor, port=5601) as server:
            health = server.health_check()
            assert "version" in health
            assert "coglet" in health["version"]
            assert "python" in health["version"]
            assert "cog" in health["version"]


class TestSyncPredictor:
    """Tests for sync predictor."""

    def test_basic_prediction(self, sync_predictor: Path):
        with CogletServer(sync_predictor, port=5602) as server:
            result = server.predict({"name": "Claude"})
            assert result["status"] == "succeeded"
            assert result["output"] == "Hello, Claude!"

    def test_default_input(self, sync_predictor: Path):
        with CogletServer(sync_predictor, port=5603) as server:
            result = server.predict({})
            assert result["status"] == "succeeded"
            assert result["output"] == "Hello, World!"

    def test_includes_predict_time(self, sync_predictor: Path):
        with CogletServer(sync_predictor, port=5604) as server:
            result = server.predict({"name": "test"})
            assert "metrics" in result
            assert "predict_time" in result["metrics"]
            assert result["metrics"]["predict_time"] >= 0


class TestGeneratorPredictor:
    """Tests for generator predictor."""

    def test_returns_array_output(self, generator_predictor: Path):
        with CogletServer(generator_predictor, port=5605) as server:
            result = server.predict({"count": 3})
            assert result["status"] == "succeeded"
            assert result["output"] == ["chunk 0", "chunk 1", "chunk 2"]

    def test_custom_count(self, generator_predictor: Path):
        with CogletServer(generator_predictor, port=5606) as server:
            result = server.predict({"count": 5})
            assert len(result["output"]) == 5


class TestAsyncPredictor:
    """Tests for async predictor."""

    def test_basic_prediction(self, async_predictor: Path):
        with CogletServer(async_predictor, port=5607) as server:
            result = server.predict({"delay": 0.1, "name": "async"})
            assert result["status"] == "succeeded"
            assert "async: done" in result["output"]

    def test_sequential_requests(self, async_predictor: Path):
        """Sequential requests both succeed (subprocess isolation means no concurrency)."""
        with CogletServer(async_predictor, port=5608) as server:
            # Run two sequential requests
            result1 = server.predict({"delay": 0.1, "name": "req1"})
            result2 = server.predict({"delay": 0.1, "name": "req2"})

            assert result1["status"] == "succeeded"
            assert result2["status"] == "succeeded"
            assert "req1" in result1["output"]
            assert "req2" in result2["output"]


class TestAsyncGeneratorPredictor:
    """Tests for async generator predictor."""

    def test_returns_array_output(self, async_generator_predictor: Path):
        with CogletServer(async_generator_predictor, port=5609) as server:
            result = server.predict({"count": 3, "delay": 0.01})
            assert result["status"] == "succeeded"
            assert result["output"] == [
                "async chunk 0",
                "async chunk 1",
                "async chunk 2",
            ]


class TestCancellation:
    """Tests for prediction cancellation."""

    def test_cancel_endpoint_returns_404_for_unknown_id(self, sync_predictor: Path):
        """Test that cancelling an unknown prediction returns 404."""
        with CogletServer(sync_predictor, port=5621) as server:
            resp = requests.post(f"{server.base_url}/predictions/unknown-id/cancel")
            assert resp.status_code == 404
            result = resp.json()
            assert result == {}

    def test_prediction_response_includes_id(self, sync_predictor: Path):
        """Test that prediction responses include an ID."""
        with CogletServer(sync_predictor, port=5622) as server:
            result = server.predict({"name": "test"})
            assert "id" in result
            assert result["id"].startswith("pred_")
