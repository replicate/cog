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
                if resp.status_code == 200:
                    return
            except requests.exceptions.ConnectionError:
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


@pytest.fixture
def path_input_predictor(tmp_path: Path) -> Path:
    """Create a predictor that takes cog.Path input."""
    predictor = tmp_path / "predict.py"
    predictor.write_text("""
from cog import BasePredictor, Path

class Predictor(BasePredictor):
    def setup(self):
        pass
    
    def predict(self, file: Path) -> str:
        # Read the file and return its contents
        with open(file, 'r') as f:
            return f.read().strip()
""")
    return predictor


@pytest.fixture
def path_list_input_predictor(tmp_path: Path) -> Path:
    """Create a predictor that takes a list of cog.Path inputs."""
    predictor = tmp_path / "predict.py"
    predictor.write_text("""
from cog import BasePredictor, Path
from typing import List

class Predictor(BasePredictor):
    def setup(self):
        pass
    
    def predict(self, files: List[Path]) -> str:
        # Read all files and concatenate contents
        contents = []
        for file in files:
            with open(file, 'r') as f:
                contents.append(f.read().strip())
        return " | ".join(contents)
""")
    return predictor


class TestPathInput:
    """Tests for cog.Path input handling."""

    def test_path_from_url(self, path_input_predictor: Path, httpserver):
        """Test that URL inputs are downloaded and passed as local paths."""
        # Set up a mock HTTP server with test content
        test_content = "Hello from URL!"
        httpserver.expect_request("/test.txt").respond_with_data(test_content)

        with CogletServer(path_input_predictor, port=5610) as server:
            result = server.predict({"file": httpserver.url_for("/test.txt")})
            if result["status"] == "failed":
                print(f"Error: {result.get('error', 'unknown')}")
            assert result["status"] == "succeeded", (
                f"Failed with: {result.get('error')}"
            )
            assert result["output"] == test_content

    def test_path_list_from_urls(self, path_list_input_predictor: Path, httpserver):
        """Test that list of URL inputs are downloaded in parallel."""
        # Set up mock endpoints
        httpserver.expect_request("/file1.txt").respond_with_data("content1")
        httpserver.expect_request("/file2.txt").respond_with_data("content2")
        httpserver.expect_request("/file3.txt").respond_with_data("content3")

        with CogletServer(path_list_input_predictor, port=5611) as server:
            result = server.predict(
                {
                    "files": [
                        httpserver.url_for("/file1.txt"),
                        httpserver.url_for("/file2.txt"),
                        httpserver.url_for("/file3.txt"),
                    ]
                }
            )
            assert result["status"] == "succeeded"
            assert result["output"] == "content1 | content2 | content3"

    def test_path_from_data_uri(self, path_input_predictor: Path):
        """Test that data: URIs are handled correctly."""
        import base64

        test_content = "Hello from data URI!"
        data_uri = (
            f"data:text/plain;base64,{base64.b64encode(test_content.encode()).decode()}"
        )

        with CogletServer(path_input_predictor, port=5612) as server:
            result = server.predict({"file": data_uri})
            assert result["status"] == "succeeded"
            assert result["output"] == test_content


@pytest.fixture
def path_output_predictor(tmp_path: Path) -> Path:
    """Create a predictor that returns cog.Path output."""
    predictor = tmp_path / "predict.py"
    predictor.write_text("""
from cog import BasePredictor, Path
import tempfile

class Predictor(BasePredictor):
    def setup(self):
        pass
    
    def predict(self, text: str = "hello") -> Path:
        # Create a temp file with the text content
        f = tempfile.NamedTemporaryFile(mode='w', suffix='.txt', delete=False)
        f.write(text)
        f.close()
        return Path(f.name)
""")
    return predictor


@pytest.fixture
def path_list_output_predictor(tmp_path: Path) -> Path:
    """Create a predictor that returns a list of cog.Path outputs."""
    predictor = tmp_path / "predict.py"
    predictor.write_text("""
from cog import BasePredictor, Path
from typing import List
import tempfile

class Predictor(BasePredictor):
    def setup(self):
        pass
    
    def predict(self, count: int = 2) -> List[Path]:
        paths = []
        for i in range(count):
            f = tempfile.NamedTemporaryFile(mode='w', suffix='.txt', delete=False)
            f.write(f"file {i}")
            f.close()
            paths.append(Path(f.name))
        return paths
""")
    return predictor


@pytest.fixture
def secret_input_predictor(tmp_path: Path) -> Path:
    """Create a predictor that takes cog.Secret input."""
    predictor = tmp_path / "predict.py"
    predictor.write_text("""
from cog import BasePredictor, Secret

class Predictor(BasePredictor):
    def setup(self):
        pass
    
    def predict(self, api_key: Secret) -> str:
        # Return the secret value (in real code, you'd use it, not return it)
        return f"key_length={len(api_key.get_secret_value())}"
""")
    return predictor


class TestSecretInput:
    """Tests for cog.Secret input handling."""

    def test_secret_input(self, secret_input_predictor: Path):
        """Test that Secret inputs are handled correctly."""
        with CogletServer(secret_input_predictor, port=5615) as server:
            result = server.predict({"api_key": "my-secret-api-key"})
            assert result["status"] == "succeeded"
            assert result["output"] == "key_length=17"


@pytest.fixture
def slow_predictor(tmp_path: Path) -> Path:
    """Create a slow predictor that can be cancelled."""
    predictor = tmp_path / "predict.py"
    predictor.write_text("""
from cog import BasePredictor
import time

class Predictor(BasePredictor):
    def setup(self):
        pass
    
    def predict(self, sleep_time: float = 10.0) -> str:
        time.sleep(sleep_time)
        return "completed"
""")
    return predictor


class TestCancellation:
    """Tests for prediction cancellation."""

    def test_cancel_endpoint_returns_404_for_unknown_id(self, sync_predictor: Path):
        """Test that cancelling an unknown prediction returns 404."""
        with CogletServer(sync_predictor, port=5621) as server:
            resp = requests.post(f"{server.base_url}/predictions/unknown-id/cancel")
            assert resp.status_code == 404
            result = resp.json()
            assert result["status"] == "failed"
            assert "not found" in result["error"]

    def test_prediction_response_includes_id(self, sync_predictor: Path):
        """Test that prediction responses include an ID."""
        with CogletServer(sync_predictor, port=5622) as server:
            result = server.predict({"name": "test"})
            assert "id" in result
            assert result["id"].startswith("pred_")

    @pytest.mark.skip(
        reason="Sync cancellation requires subprocess isolation (like cog) - architectural limitation"
    )
    def test_sigusr1_cancels_sync_prediction(self, slow_predictor: Path):
        """Test that SIGUSR1 cancels a sync prediction.

        NOTE: This test is skipped because sync predictor cancellation requires
        subprocess isolation like cog does. In our in-process model:
        - Signal handlers only run between Python bytecode instructions
        - Blocking syscalls (time.sleep, I/O) can't be interrupted
        - The signal may be delivered to tokio threads, not Python

        Async predictors CAN be cancelled via asyncio.Task.cancel().
        """
        import signal
        import threading

        with CogletServer(slow_predictor, port=5620) as server:
            # Start a prediction in a thread
            result_holder = {}

            def make_prediction():
                result_holder["result"] = server.predict({"sleep_time": 30.0})

            predict_thread = threading.Thread(target=make_prediction)
            predict_thread.start()

            # Wait a bit for prediction to start
            time.sleep(0.5)

            # Send SIGUSR1 to cancel
            import os

            os.kill(server.process.pid, signal.SIGUSR1)

            # Wait for the prediction to complete (should be fast due to cancel)
            predict_thread.join(timeout=5.0)
            assert not predict_thread.is_alive(), (
                "Prediction should have been cancelled"
            )

            # Check the result
            result = result_holder.get("result", {})
            assert result.get("status") == "canceled", (
                f"Expected canceled, got: {result}"
            )


class TestPathOutput:
    """Tests for cog.Path output handling."""

    def test_path_output_as_base64(self, path_output_predictor: Path):
        """Test that Path outputs are converted to base64 data URLs."""
        import base64

        with CogletServer(path_output_predictor, port=5613) as server:
            result = server.predict({"text": "hello world"})
            assert result["status"] == "succeeded"

            # Output should be a data URL
            output = result["output"]
            assert output.startswith("data:"), f"Expected data URL, got: {output}"
            assert ";base64," in output

            # Decode and verify content
            _, encoded = output.split(";base64,", 1)
            decoded = base64.b64decode(encoded).decode("utf-8")
            assert decoded == "hello world"

    def test_path_list_output_as_base64(self, path_list_output_predictor: Path):
        """Test that list of Path outputs are converted to base64 data URLs."""
        import base64

        with CogletServer(path_list_output_predictor, port=5614) as server:
            result = server.predict({"count": 3})
            assert result["status"] == "succeeded"

            # Output should be a list of data URLs
            output = result["output"]
            assert isinstance(output, list)
            assert len(output) == 3

            for i, item in enumerate(output):
                assert item.startswith("data:"), f"Expected data URL, got: {item}"
                _, encoded = item.split(";base64,", 1)
                decoded = base64.b64decode(encoded).decode("utf-8")
                assert decoded == f"file {i}"
