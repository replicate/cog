"""Tests for coglet Python bindings."""

import queue
import re
import socket
import subprocess
import sys
import threading
import time
from pathlib import Path

import coglet
import pytest
import requests

# =============================================================================
# Module structure tests (no server needed)
# =============================================================================


class TestModuleStructure:
    """Tests for coglet module public API and structure."""

    def test_version_is_pep440(self) -> None:
        """__version__ must be a valid PEP 440 version string."""
        # PEP 440: N.N.N, N.N.NaN, N.N.NbN, N.N.NrcN, N.N.N.devN, etc.
        assert re.match(
            r"^\d+\.\d+\.\d+(\.dev\d+|a\d+|b\d+|rc\d+)?(\+.+)?$",
            coglet.__version__,
        ), f"Not PEP 440: {coglet.__version__!r}"

    def test_version_is_str(self) -> None:
        assert isinstance(coglet.__version__, str)

    def test_build_info_exists(self) -> None:
        build = coglet.__build__
        assert hasattr(build, "version")
        assert hasattr(build, "git_sha")
        assert hasattr(build, "build_time")
        assert hasattr(build, "rustc_version")

    def test_build_info_fields_are_strings(self) -> None:
        build = coglet.__build__
        assert isinstance(build.version, str)
        assert isinstance(build.git_sha, str)
        assert isinstance(build.build_time, str)
        assert isinstance(build.rustc_version, str)

    def test_build_info_version_matches_module_version(self) -> None:
        assert coglet.__build__.version == coglet.__version__

    def test_build_info_repr(self) -> None:
        r = repr(coglet.__build__)
        assert r.startswith("BuildInfo(")
        assert "version=" in r
        assert "git_sha=" in r

    def test_build_info_frozen(self) -> None:
        with pytest.raises(AttributeError):
            coglet.__build__.version = "hacked"  # type: ignore[misc]

    def test_server_exists(self) -> None:
        assert hasattr(coglet, "server")

    def test_server_active_is_false(self) -> None:
        """Outside a worker subprocess, active should be False."""
        assert coglet.server.active is False

    def test_server_active_is_property(self) -> None:
        """active should be a property (no parens needed), not callable."""
        assert isinstance(coglet.server.active, bool)

    def test_server_frozen(self) -> None:
        with pytest.raises(AttributeError):
            coglet.server.foo = "bar"  # type: ignore[attr-defined]

    def test_server_active_not_settable(self) -> None:
        with pytest.raises(AttributeError):
            coglet.server.active = True  # type: ignore[misc]

    def test_server_repr(self) -> None:
        assert repr(coglet.server) == "coglet.server"

    def test_sdk_submodule_exists(self) -> None:
        assert hasattr(coglet, "_sdk")

    def test_sdk_has_slot_log_writer(self) -> None:
        assert hasattr(coglet._sdk, "_SlotLogWriter")

    def test_sdk_has_tee_writer(self) -> None:
        assert hasattr(coglet._sdk, "_TeeWriter")

    def test_all_excludes_internals(self) -> None:
        """__all__ should only list public API."""
        assert "__version__" in coglet.__all__
        assert "__build__" in coglet.__all__
        assert "server" in coglet.__all__
        # _sdk should not be in __all__ (underscore = private)
        assert "_sdk" not in coglet.__all__
        assert "_impl" not in coglet.__all__


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

    # Create cog.yaml
    cog_yaml = tmp_path / "cog.yaml"
    cog_yaml.write_text("""
predict: "predict.py:Predictor"
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

    # Create cog.yaml
    cog_yaml = tmp_path / "cog.yaml"
    cog_yaml.write_text("""
predict: "predict.py:Predictor"
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

    # Create cog.yaml
    cog_yaml = tmp_path / "cog.yaml"
    cog_yaml.write_text("""
predict: "predict.py:Predictor"
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

    # Create cog.yaml
    cog_yaml = tmp_path / "cog.yaml"
    cog_yaml.write_text("""
predict: "predict.py:Predictor"
""")

    return predictor


class CogletServer:
    """Context manager for running coglet server."""

    def __init__(self, predictor_path: Path, port: int = 0):
        self.predictor_path = predictor_path
        self.requested_port = port
        self.port = None
        self.process = None
        self.stderr_lines = []
        self.stderr_queue = queue.Queue()
        self.stderr_thread = None

    def __enter__(self):
        cmd = [
            sys.executable,
            "-c",
            f"import coglet; coglet.server.serve('{self.predictor_path}:Predictor', port={self.requested_port})",
        ]
        self.process = subprocess.Popen(
            cmd,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1,  # Line buffered
            cwd=str(
                self.predictor_path.parent
            ),  # Run from predictor directory to find cog.yaml
        )

        # Start background thread to read stderr
        self.stderr_thread = threading.Thread(target=self._read_stderr, daemon=True)
        self.stderr_thread.start()

        # Discover actual port from logs
        self._discover_port()
        # Wait for server to become ready
        self._wait_for_ready()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        if self.process:
            self.process.terminate()
            self.process.wait(timeout=5)

    def _read_stderr(self):
        """Background thread that reads stderr and queues lines."""
        try:
            for line in self.process.stderr:
                self.stderr_lines.append(line)
                self.stderr_queue.put(line)
        except Exception:
            pass  # Process terminated

    def _discover_port(self, timeout: float = 5.0):
        """Read stderr until we find the port the server bound to."""
        start = time.time()
        while time.time() - start < timeout:
            try:
                line = self.stderr_queue.get(timeout=0.1)
            except queue.Empty:
                if self.process.poll() is not None:
                    # Process died
                    raise RuntimeError(
                        f"Server process died during startup\nSTDERR:\n{''.join(self.stderr_lines)}"
                    )
                continue

            # Look for: "Starting coglet server on 0.0.0.0:PORT"
            match = re.search(r"Starting coglet server on [\d.]+:(\d+)", line)
            if match:
                self.port = int(match.group(1))
                return

        raise TimeoutError(
            f"Could not discover server port within {timeout}s\nSTDERR:\n{''.join(self.stderr_lines)}"
        )

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

        # Terminate on failure
        if self.process and self.process.poll() is None:
            self.process.terminate()
            self.process.wait(timeout=2)

        raise TimeoutError(
            f"Server did not become ready within {timeout}s (port={self.port})\n"
            f"STDERR:\n{''.join(self.stderr_lines)}"
        )

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
        with CogletServer(sync_predictor) as server:
            health = server.health_check()
            assert health["status"] == "READY"

    def test_returns_version_info(self, sync_predictor: Path):
        with CogletServer(sync_predictor) as server:
            health = server.health_check()
            assert "version" in health
            assert "coglet" in health["version"]
            assert "python" in health["version"]
            assert "cog" in health["version"]


class TestSyncPredictor:
    """Tests for sync predictor."""

    def test_basic_prediction(self, sync_predictor: Path):
        with CogletServer(sync_predictor) as server:
            result = server.predict({"name": "Claude"})
            assert result["status"] == "succeeded"
            assert result["output"] == "Hello, Claude!"

    def test_default_input(self, sync_predictor: Path):
        with CogletServer(sync_predictor) as server:
            result = server.predict({})
            assert result["status"] == "succeeded"
            assert result["output"] == "Hello, World!"

    def test_includes_predict_time(self, sync_predictor: Path):
        with CogletServer(sync_predictor) as server:
            result = server.predict({"name": "test"})
            assert "metrics" in result
            assert "predict_time" in result["metrics"]
            assert result["metrics"]["predict_time"] >= 0


class TestGeneratorPredictor:
    """Tests for generator predictor."""

    def test_returns_array_output(self, generator_predictor: Path):
        with CogletServer(generator_predictor) as server:
            result = server.predict({"count": 3})
            assert result["status"] == "succeeded"
            assert result["output"] == ["chunk 0", "chunk 1", "chunk 2"]

    def test_custom_count(self, generator_predictor: Path):
        with CogletServer(generator_predictor) as server:
            result = server.predict({"count": 5})
            assert len(result["output"]) == 5


class TestAsyncPredictor:
    """Tests for async predictor."""

    def test_basic_prediction(self, async_predictor: Path):
        with CogletServer(async_predictor) as server:
            result = server.predict({"delay": 0.1, "name": "async"})
            assert result["status"] == "succeeded"
            assert "async: done" in result["output"]

    def test_sequential_requests(self, async_predictor: Path):
        """Sequential requests both succeed (subprocess isolation means no concurrency)."""
        with CogletServer(async_predictor) as server:
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
        with CogletServer(async_generator_predictor) as server:
            result = server.predict({"count": 3, "delay": 0.01})
            assert result["status"] == "succeeded"
            assert result["output"] == [
                "async chunk 0",
                "async chunk 1",
                "async chunk 2",
            ]


@pytest.fixture
def slow_sync_predictor(tmp_path: Path) -> Path:
    """Create a sync predictor that busy-loops (cancellable at bytecode boundaries)."""
    predictor = tmp_path / "predict.py"
    predictor.write_text("""
import time
from cog import BasePredictor

class Predictor(BasePredictor):
    def setup(self):
        pass

    def predict(self, duration: float = 60.0) -> str:
        # Busy-loop in Python (hits bytecode boundaries, so PyThreadState_SetAsyncExc works)
        deadline = time.monotonic() + duration
        while time.monotonic() < deadline:
            pass
        return "completed"
""")

    cog_yaml = tmp_path / "cog.yaml"
    cog_yaml.write_text("""
predict: "predict.py:Predictor"
""")

    return predictor


@pytest.fixture
def slow_async_predictor(tmp_path: Path) -> Path:
    """Create an async predictor that sleeps for a long time (cancellable)."""
    predictor = tmp_path / "predict.py"
    predictor.write_text("""
import asyncio
from cog import BasePredictor

class Predictor(BasePredictor):
    def setup(self):
        pass

    async def predict(self, sleep_time: float = 60.0) -> str:
        await asyncio.sleep(sleep_time)
        return "completed"
""")

    cog_yaml = tmp_path / "cog.yaml"
    cog_yaml.write_text("""
predict: "predict.py:Predictor"
""")

    return predictor


def _wait_for_health_status(
    server: "CogletServer", status: str, timeout: float = 5.0
) -> None:
    """Poll health check until the expected status is reached, or fail."""
    deadline = time.time() + timeout
    last_status = "<unknown>"
    while time.time() < deadline:
        health = server.health_check()
        last_status = health["status"]
        if last_status == status:
            return
        time.sleep(0.1)
    stderr = "".join(server.stderr_lines)
    pytest.fail(
        f"Server did not reach status {status!r} within {timeout}s\n"
        f"Last status: {last_status!r}\n"
        f"STDERR:\n{stderr}"
    )


class TestCancellation:
    """Tests for prediction cancellation."""

    def test_cancel_endpoint_returns_404_for_unknown_id(self, sync_predictor: Path):
        """Test that cancelling an unknown prediction returns 404."""
        with CogletServer(sync_predictor) as server:
            resp = requests.post(f"{server.base_url}/predictions/unknown-id/cancel")
            assert resp.status_code == 404
            result = resp.json()
            assert result == {}

    def test_prediction_response_includes_id(self, sync_predictor: Path):
        """Test that prediction responses include an ID."""
        with CogletServer(sync_predictor) as server:
            result = server.predict({"name": "test"})
            assert "id" in result
            assert result["id"].startswith("pred_")

    def test_cancel_running_sync_prediction(self, slow_sync_predictor: Path):
        """Test that cancelling a running sync prediction actually terminates it."""
        with CogletServer(slow_sync_predictor) as server:
            # Start a long-running prediction asynchronously
            prediction_id = "cancel-sync-test"
            resp = requests.put(
                f"{server.base_url}/predictions/{prediction_id}",
                json={"input": {"duration": 60.0}},
                headers={"Prefer": "respond-async"},
            )
            assert resp.status_code == 202

            # Wait for the prediction to actually be processing (slot occupied)
            _wait_for_health_status(server, "BUSY", timeout=5.0)

            # Cancel the prediction
            cancel_resp = requests.post(
                f"{server.base_url}/predictions/{prediction_id}/cancel"
            )
            assert cancel_resp.status_code == 200

            # Wait for the server to return to READY (slot freed after cancel)
            _wait_for_health_status(server, "READY", timeout=10.0)

    def test_cancel_running_async_prediction(self, slow_async_predictor: Path):
        """Test that cancelling a running async prediction actually terminates it."""
        with CogletServer(slow_async_predictor) as server:
            # Start a long-running async prediction
            prediction_id = "cancel-async-test"
            resp = requests.put(
                f"{server.base_url}/predictions/{prediction_id}",
                json={"input": {"sleep_time": 60.0}},
                headers={"Prefer": "respond-async"},
            )
            assert resp.status_code == 202

            # Wait for the prediction to actually be processing (slot occupied)
            _wait_for_health_status(server, "BUSY", timeout=5.0)

            # Cancel the prediction
            cancel_resp = requests.post(
                f"{server.base_url}/predictions/{prediction_id}/cancel"
            )
            assert cancel_resp.status_code == 200

            # Wait for the server to return to READY (slot freed after cancel)
            _wait_for_health_status(server, "READY", timeout=10.0)

    def test_cancel_sync_prediction_connection_drop(self, slow_sync_predictor: Path):
        """Test that dropping a sync connection cancels the prediction."""
        with CogletServer(slow_sync_predictor) as server:
            # Start a sync (non-async) prediction with a short timeout
            # The connection drop should trigger cancellation via SyncPredictionGuard
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.connect(("localhost", server.port))

            request_body = '{"input": {"duration": 60.0}}'
            http_request = (
                f"POST /predictions HTTP/1.1\r\n"
                f"Host: localhost:{server.port}\r\n"
                f"Content-Type: application/json\r\n"
                f"Content-Length: {len(request_body)}\r\n"
                f"\r\n"
                f"{request_body}"
            )
            sock.sendall(http_request.encode())

            # Wait for the prediction to be processing (slot occupied)
            _wait_for_health_status(server, "BUSY", timeout=5.0)

            # Drop the connection abruptly
            sock.close()

            # Wait for the server to return to READY (slot freed after cancel)
            _wait_for_health_status(server, "READY", timeout=10.0)
