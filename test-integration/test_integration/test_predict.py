import asyncio
import os
import time
from pathlib import Path

import httpx
import pytest
from pytest_httpserver import HTTPServer
from werkzeug import Request, Response

from .util import cog_server_http_run


@pytest.mark.asyncio
async def test_concurrent_predictions(cog_binary):
    """Test that concurrent async predictions complete properly with server shutdown.

    This test verifies:
    1. Multiple predictions can run concurrently
    2. Server shutdown waits for running predictions to complete
    3. All predictions return correct results

    This test is kept in Python because it requires:
    - Async HTTP client (httpx.AsyncClient)
    - asyncio.TaskGroup for concurrent requests
    - Precise timing verification
    """

    async def make_request(i: int) -> httpx.Response:
        return await client.post(
            f"{addr}/predictions",
            json={
                "id": f"id-{i}",
                "input": {"s": f"sleepyhead{i}", "sleep": 1.0},
            },
        )

    with cog_server_http_run(
        Path(__file__).parent / "fixtures" / "async-sleep-project", cog_binary
    ) as addr:
        async with httpx.AsyncClient() as client:
            tasks = []
            start = time.perf_counter()
            async with asyncio.TaskGroup() as tg:
                for i in range(5):
                    tasks.append(tg.create_task(make_request(i)))
                # give time for all of the predictions to be accepted, but not completed
                await asyncio.sleep(0.2)
                # we shut the server down, but expect all running predictions to complete
                await client.post(f"{addr}/shutdown")
            end = time.perf_counter()
            assert (end - start) < 3.0  # ensure the predictions ran concurrently
            for i, task in enumerate(tasks):
                assert task.result().status_code == 200
                assert task.result().json()["output"] == f"wake up sleepyhead{i}"


def test_predict_pipeline_downloaded_requirements(cog_binary):
    """Test that pipeline builds download runtime requirements and make dependencies available.

    This test is kept in Python because it requires:
    - pytest_httpserver for mock HTTP server
    - Complex environment variable setup
    - Verification of downloaded requirements content
    """
    project_dir = Path(__file__).parent / "fixtures/pipeline-requirements-project"

    # Create initial local requirements.txt that differs from what mock server will return
    # This simulates the out-of-sync scenario
    initial_local_requirements = """# pipelines-runtime@sha256:d1b9fbd673288453fdf12806f4cba9e9e454f0f89b187eac2db5731792f71d60
moviepy==v2.2.1
numpy==v2.3.2
pillow==v11.3.0
pydantic==v1.10.22
replicate==v2.0.0a22
requests==v2.32.5
scikit-learn==v1.7.1
"""

    # Create a mock requirements file that includes basic packages needed for validation
    mock_requirements = """# Mock runtime requirements for testing
requests==2.32.5
urllib3==2.0.4
"""

    # Write the initial local requirements file (will be overwritten during test)
    requirements_file = project_dir / "requirements.txt"
    requirements_file.write_text(initial_local_requirements)

    try:
        # Set up a mock HTTP server to serve the requirements file
        with HTTPServer(host="127.0.0.1", port=0) as httpserver:

            def requirements_handler(request: Request) -> Response:
                if request.path == "/requirements.txt":
                    # Include ETag header as expected by the requirements download logic
                    headers = {"ETag": '"mock-requirements-etag-123"'}
                    return Response(
                        mock_requirements,
                        status=200,
                        headers=headers,
                        content_type="text/plain",
                    )
                return Response("Not Found", status=404)

            httpserver.expect_request("/requirements.txt").respond_with_handler(
                requirements_handler
            )

            # Get the server URL (context manager already started the server)
            server_host = f"127.0.0.1:{httpserver.port}"

            # Run prediction with pipeline flag and mock server
            import subprocess

            env = os.environ.copy()
            env["R8_PIPELINES_RUNTIME_HOST"] = server_host
            env["R8_SCHEME"] = "http"  # Use HTTP instead of HTTPS for testing

            result = subprocess.run(
                [cog_binary, "predict", "--x-pipeline", "--debug"],
                cwd=project_dir,
                capture_output=True,
                text=True,
                timeout=120.0,
                env=env,
            )

            # Should succeed since packages should be available from downloaded requirements
            assert result.returncode == 0

            # The output should list all installed packages (one per line)
            assert (
                len(result.stdout.strip().split("\n")) > 10
            )  # Should have many packages

            # Should not contain error messages
            assert "ERROR:" not in result.stdout

            # Verify that the mock requirements were downloaded by checking debug output
            assert "Generated requirements.txt:" in result.stderr
            # Should show requests from our mock requirements (proves download worked)
            assert "requests==2.32.5" in result.stderr

            # Verify that specific versions from mock are present in the installed packages list
            assert "requests==2.32.5" in result.stdout
            assert (
                "urllib3==" in result.stdout
            )  # Just check that urllib3 is present with some version

            # Verify that the local requirements.txt file was updated with mock requirements
            local_requirements_path = project_dir / "requirements.txt"
            with open(local_requirements_path, "r") as f:
                local_requirements_content = f.read()
            # Should contain our mock requirements
            assert "requests==2.32.5" in local_requirements_content
            assert (
                "# Mock runtime requirements for testing" in local_requirements_content
            )

    finally:
        # Clean up: remove the dynamic requirements.txt file so it doesn't affect git
        if requirements_file.exists():
            requirements_file.unlink()
