import contextlib
import random
import signal
import socket
import string
import subprocess
import sys
import time

import httpx


def random_string(length: int) -> str:
    return "".join(random.choice(string.ascii_lowercase) for _ in range(length))


def remove_docker_image(
    image_name: str, max_attempts: int = 5, wait_seconds: int = 1
) -> None:
    for attempt in range(max_attempts):
        try:
            subprocess.run(
                ["docker", "rmi", "-f", image_name], check=True, capture_output=True
            )
            print(f"Image {image_name} successfully removed.")
            break
        except subprocess.CalledProcessError as exc:
            print(f"Attempt {attempt + 1} failed: {exc.stderr.decode()}")
            time.sleep(wait_seconds)
    else:
        print(f"Failed to remove image {image_name} after {max_attempts} attempts.")


def random_port() -> int:
    sock = socket.socket()
    sock.bind(("127.0.0.1", 0))
    port = sock.getsockname()[1]
    sock.close()
    return port


@contextlib.contextmanager
def cog_server_http_run(project_dir: str, cog_binary: str):
    port = random_port()
    addr = f"http://127.0.0.1:{port}"

    server: subprocess.Popen[bytes] | None = None

    try:
        server = subprocess.Popen(
            [
                cog_binary,
                "serve",
                "-p",
                str(port),
            ],
            cwd=project_dir,
            # NOTE: inheriting stdout and stderr from the parent process when running
            # within a pytest context seems to *always fail*, even when using
            # `capsys.disabled` or `--capture=no` via command line args. Piping the
            # streams seems to allow behavior that is identical to code run outside of
            # pytest.
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )

        i = 0

        while True:
            try:
                if httpx.get(f"{addr}/health-check").status_code == 200:
                    break
            except httpx.HTTPError:
                pass

            time.sleep((0.1 + i) * 2)
            i += 1

        yield addr
    finally:
        try:
            httpx.post(f"{addr}/shutdown")
        except httpx.HTTPError:
            pass

        if server is not None:
            server.send_signal(signal.SIGINT)

            out, err = server.communicate(timeout=5)

            if server.returncode != 0:
                print(out.decode())
                print(err.decode(), file=sys.stderr)
