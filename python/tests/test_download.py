"""Regressions for File/Path input SSRF; all HTTP listeners are local."""

import io
import os
import socket
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from types import SimpleNamespace

import pytest
import requests

from cog.types import File, Path, URLFile, URLPath

PUBLIC_IP = "8.8.8.8"


@pytest.fixture(params=["file", "path", "urlfile"])
def download(request):
    if request.param == "path" and os.name == "nt":
        pytest.skip("Cog Path requires POSIX")

    def read(url):
        if request.param == "path":
            value = Path.validate(url)
            try:
                return value.convert().read_bytes()
            finally:
                value.unlink(missing_ok=True)
        value = File.validate(url) if request.param == "file" else URLFile(url)
        with value:
            return value.read()

    return read


@pytest.fixture
def listener():
    servers = []

    def start(redirect=None):
        hits = []

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                hits.append(dict(self.headers))
                self.send_response(302 if redirect else 200)
                self.send_header("Content-Length", "2")
                if redirect:
                    self.send_header("Location", redirect)
                self.end_headers()
                self.wfile.write(b"ok")

            def log_message(self, *args):
                pass

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(
            target=server.serve_forever, kwargs={"poll_interval": 0.01}, daemon=True
        )
        thread.start()
        servers.append((server, thread))
        return SimpleNamespace(address=server.server_address, hits=hits)

    yield start
    for server, thread in servers:
        server.shutdown()
        server.server_close()
        thread.join()


def answers(*addresses):
    return [
        (
            socket.AF_INET6 if ":" in ip else socket.AF_INET,
            socket.SOCK_STREAM,
            socket.IPPROTO_TCP,
            "",
            (ip, 80),
        )
        for ip in addresses
    ]


def route_public(monkeypatch, origin):
    """Route only the validated numeric address to the controlled origin."""
    real_socket = socket.socket
    connections = []

    class RoutedSocket(real_socket):
        def connect(self, address):
            connections.append(address)
            assert address[0] == PUBLIC_IP
            return super().connect(origin.address)

    monkeypatch.setattr(socket, "socket", RoutedSocket)
    return connections


@pytest.mark.parametrize(
    "host",
    [
        "127.0.0.1",
        "localhost",
        "10.0.0.1",
        "169.254.169.254",
        "168.63.129.16",
        "224.0.0.1",
        "[::1]",
        "[::ffff:127.0.0.1]",
    ],
)
def test_internal_input_is_rejected(download, listener, host):
    blocked = listener()
    with pytest.raises(ValueError, match="Blocked input download"):
        download(f"http://{host}:{blocked.address[1]}/secret")
    assert blocked.hits == []


@pytest.mark.parametrize(
    "addresses",
    [
        (PUBLIC_IP, "127.0.0.1"),
        ("2606:4700::1111", "::1"),
    ],
)
def test_mixed_dns_is_rejected(download, listener, monkeypatch, addresses):
    blocked = listener()
    monkeypatch.setattr(socket, "getaddrinfo", lambda *a, **kw: answers(*addresses))
    with pytest.raises(ValueError, match="Blocked input download"):
        download(f"http://download.test:{blocked.address[1]}/secret")
    assert blocked.hits == []


def test_resolution_failure_is_rejected(download, monkeypatch):
    def resolve(*args, **kwargs):
        raise socket.gaierror("test resolution failure")

    monkeypatch.setattr(socket, "getaddrinfo", resolve)
    with pytest.raises(ValueError, match="Unable to resolve"):
        download("http://download.test/file")


@pytest.mark.parametrize(
    "url",
    [
        "http:///file",
        "http://user:password@download.test/file",
        "http://download.test:65536/file",
    ],
)
def test_invalid_input_url(download, url):
    with pytest.raises(ValueError):
        download(url)


def test_allowed_download_is_pinned_and_bypasses_proxies(
    download, listener, monkeypatch
):
    origin, blocked = listener(), listener()
    connections = route_public(monkeypatch, origin)
    lookups = []

    def resolve(host, port, **kwargs):
        lookups.append((host, port))
        return answers(PUBLIC_IP if len(lookups) == 1 else "127.0.0.1")

    monkeypatch.setattr(socket, "getaddrinfo", resolve)
    for name in (
        "HTTP_PROXY",
        "HTTPS_PROXY",
        "ALL_PROXY",
        "http_proxy",
        "https_proxy",
        "all_proxy",
    ):
        monkeypatch.setenv(name, f"http://127.0.0.1:{blocked.address[1]}")
    monkeypatch.setenv("NO_PROXY", "")
    monkeypatch.setenv("no_proxy", "")
    assert download("http://download.test:8080/file") == b"ok"
    assert lookups == [("download.test", 8080)]
    assert connections == [(PUBLIC_IP, 8080)]
    assert origin.hits[0]["Host"] == "download.test:8080"
    assert blocked.hits == []


def test_redirect_is_rejected(download, listener, monkeypatch):
    blocked = listener()
    origin = listener(redirect=f"http://127.0.0.1:{blocked.address[1]}/secret")
    route_public(monkeypatch, origin)
    monkeypatch.setattr(socket, "getaddrinfo", lambda *a, **kw: answers(PUBLIC_IP))
    with pytest.raises(ValueError, match="redirects are not allowed"):
        download("http://download.test/file")
    assert len(origin.hits) == 1
    assert blocked.hits == []


@pytest.mark.parametrize("scheme,port", [("http", 80), ("https", 443)])
def test_fetch_preserves_request_options(monkeypatch, scheme, port):
    response = requests.Response()
    response.status_code = 200
    response.raw = io.BytesIO(b"ok")
    lookups = []

    def resolve(host, resolved_port, **kwargs):
        lookups.append((host, resolved_port))
        return answers(PUBLIC_IP)

    def send(adapter, request, **kwargs):
        assert request.url == f"{scheme}://download.test/file"
        assert kwargs["verify"] is True
        assert kwargs["stream"] is True
        assert kwargs["timeout"] == 10
        assert kwargs["proxies"] == {}
        pool = adapter.poolmanager.connection_from_url(request.url)
        assert pool.host == "download.test"
        assert pool.port == port
        return response

    monkeypatch.setattr(socket, "getaddrinfo", resolve)
    monkeypatch.setattr(requests.adapters.HTTPAdapter, "send", send)
    with URLFile(f"{scheme}://download.test/file") as file:
        assert file.read() == b"ok"
    assert response.raw.closed
    assert lookups == [("download.test", port)]


def test_data_and_local_inputs(tmp_path):
    assert File.validate("data:text/plain;base64,b2s=").read() == b"ok"
    local = tmp_path / "file.txt"
    local.write_bytes(b"ok")
    assert Path.validate(local) is local
    if os.name != "nt":
        assert Path.validate(str(local)).read_bytes() == b"ok"
        value = Path.validate("data:text/plain;base64,b2s=")
        try:
            assert value.convert().read_bytes() == b"ok"
        finally:
            value.unlink()


def test_failed_copy_removes_partial_file(tmp_path, monkeypatch):
    class BrokenFile(io.BytesIO):
        def read(self, size=-1):
            if self.tell():
                raise OSError("interrupted download")
            return super().read(3)

    source = BrokenFile(b"partial")
    value = SimpleNamespace(_path=None, filename="file.txt", fileobj=source)
    monkeypatch.setattr("tempfile.tempdir", str(tmp_path))
    with pytest.raises(OSError, match="interrupted"):
        URLPath.convert(value)
    assert list(tmp_path.iterdir()) == []
    assert source.closed
