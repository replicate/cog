import time
from contextlib import contextmanager
import json
import subprocess
import random
import re
import string
import socket
from typing import List, Optional
from contextlib import closing


def find_free_port():
    with closing(socket.socket(socket.AF_INET, socket.SOCK_STREAM)) as s:
        s.bind(("", 0))
        s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        return s.getsockname()[1]


def wait_for_port(host, port, timeout=60):
    start = time.time()
    while True:
        try:
            if time.time() - start > timeout:
                raise Exception(f"Something is wrong. Timeout waiting for port {port}")
            with socket.create_connection((host, port), timeout=5):
                return
        except socket.error:
            pass
        except socket.timeout:
            raise


def random_string(length):
    return "".join(random.choice(string.ascii_lowercase) for i in range(length))


def show_version(model_url, version_id):
    out, _ = subprocess.Popen(
        ["cog", "--model", model_url, "show", "--json", version_id],
        stdout=subprocess.PIPE,
    ).communicate()
    return json.loads(out)


def set_model_url(model_url, project_dir):
    out, _ = subprocess.Popen(
        ["cog", "model", "set", model_url],
        stdout=subprocess.PIPE,
        cwd=project_dir,
    ).communicate()
    assert out.decode() == f"Updated model: {model_url}\n"


def push_with_log(project_dir):
    out, _ = subprocess.Popen(
        ["cog", "push", "--log"],
        cwd=project_dir,
        stdout=subprocess.PIPE,
    ).communicate()

    assert "Successfully uploaded version" in out.decode()
    version_id = re.search("Successfully uploaded version (.+)", out.decode()).group(1)

    return version_id


@contextmanager
def docker_run(
    image,
    name=None,
    detach=False,
    interactive=False,
    publish: Optional[List[dict]] = None,
    command: Optional[List[str]] = None,
    env: Optional[dict] = None,
):
    if name is None:
        name = random_string(10)

    cmd = ["docker", "run", "--name", name]
    if publish is not None:
        for port_binding in publish:
            host_port = port_binding["host"]
            container_port = port_binding["container"]
            cmd += ["--publish", f"{host_port}:{container_port}"]
    if env is not None:
        for key, value in env.items():
            cmd += ["-e", f"{key}={value}"]
    if detach:
        cmd += ["--detach"]
    if interactive:
        cmd += ["-i"]
    cmd += [image]
    if command:
        cmd += command
    try:
        subprocess.Popen(cmd)
        yield
    finally:
        subprocess.Popen(["docker", "rm", "--force", name]).wait()


def get_local_ip():
    return socket.gethostbyname(socket.gethostname())
