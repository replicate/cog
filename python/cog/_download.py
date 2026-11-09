"""Fetch untrusted input URLs without allowing access to internal services."""

import ipaddress
import socket
from typing import Any, Iterator, Union, cast
from urllib.parse import urlsplit

import requests
from requests.adapters import HTTPAdapter
from urllib3.connection import HTTPConnection, HTTPSConnection
from urllib3.connectionpool import HTTPConnectionPool, HTTPSConnectionPool
from urllib3.exceptions import NewConnectionError

IPAddress = Union[ipaddress.IPv4Address, ipaddress.IPv6Address]

# Azure's platform virtual IP is globally classified, but exposes internal
# services. Other common metadata endpoints are already non-global addresses.
# https://learn.microsoft.com/azure/virtual-network/what-is-ip-address-168-63-129-16
_INTERNAL_NETWORKS = (
    ipaddress.ip_network("168.63.129.16/32"),
    ipaddress.ip_network("64:ff9b::/96"),  # NAT64 may translate to internal IPv4.
    ipaddress.ip_network("64:ff9b:1::/48"),
    ipaddress.ip_network("192.88.99.0/24"),  # Deprecated relay addresses.
    ipaddress.ip_network("3fff::/20"),  # Documentation prefix added in 2024.
    ipaddress.ip_network("fec0::/10"),  # Deprecated IPv6 site-local addresses.
)
_PROTOCOL_V4 = ipaddress.ip_network("192.0.0.0/24")
_PROTOCOL_V6 = ipaddress.ip_network("2001::/23")
_GLOBAL_PROTOCOL_V6 = tuple(
    ipaddress.ip_network(cidr)
    for cidr in (
        "2001:1::1/128",
        "2001:1::2/128",
        "2001:1::3/128",
        "2001:3::/32",
        "2001:4:112::/48",
        "2001:20::/28",
        "2001:30::/28",
    )
)


def _is_global(address: IPAddress) -> bool:
    # Older supported Python patch releases have stale classifications for
    # these IANA protocol-assignment ranges. Keep their exceptions explicit.
    # https://www.iana.org/assignments/iana-ipv4-special-registry/
    # https://www.iana.org/assignments/iana-ipv6-special-registry/
    if address in _PROTOCOL_V4:
        return str(address) in ("192.0.0.9", "192.0.0.10")
    if address in _PROTOCOL_V6:
        return any(address in network for network in _GLOBAL_PROTOCOL_V6)
    return address.is_global


def _validate_url(url: str) -> None:
    """Reject ambiguous authorities before Requests normalizes the URL."""
    try:
        parsed = urlsplit(url)
        if parsed.scheme not in ("http", "https") or not parsed.hostname:
            raise ValueError("an HTTP(S) URL with a hostname is required")
        if parsed.username is not None or parsed.password is not None:
            raise ValueError("embedded credentials are not allowed")
        if (
            any(ord(char) < 32 or ord(char) == 127 for char in url)
            or any(char.isspace() for char in parsed.netloc)
            or "\\" in parsed.netloc
            or "%" in parsed.netloc
            or parsed.netloc.endswith(":")
        ):
            raise ValueError("invalid URL authority")
        if parsed.port is not None and not 1 <= parsed.port <= 65535:
            raise ValueError("invalid port")
    except ValueError as exc:
        raise ValueError(f"Invalid input download URL: {exc}") from exc


def _validated_addresses(host: str, port: int) -> tuple[IPAddress, ...]:
    """Resolve once, rejecting the entire answer if any address is blocked."""
    try:
        literal = ipaddress.ip_address(host)
    except ValueError:
        try:
            answers = socket.getaddrinfo(host, port, type=socket.SOCK_STREAM)
            addresses = [ipaddress.ip_address(answer[4][0]) for answer in answers]
        except (OSError, ValueError) as exc:
            raise ValueError("Unable to resolve input download hostname") from exc
    else:
        addresses = [literal]

    if not addresses:
        raise ValueError("Unable to resolve input download hostname")
    normalized = []
    for address in addresses:
        if isinstance(address, ipaddress.IPv6Address):
            address = address.ipv4_mapped or address
        if (
            not _is_global(address)
            or address.is_multicast
            or address.is_reserved
            or any(address in network for network in _INTERNAL_NETWORKS)
            # Transition mechanisms can encode an otherwise blocked IPv4
            # destination, so do not permit them as public IPv6 destinations.
            or (
                isinstance(address, ipaddress.IPv6Address)
                and (address.sixtofour is not None or address.teredo is not None)
            )
        ):
            raise ValueError("Blocked input download destination")
        if address not in normalized:
            normalized.append(address)
    return tuple(normalized)


def _connect(conn: HTTPConnection, addresses: tuple[IPAddress, ...]) -> socket.socket:
    """Connect to numeric addresses without invoking DNS a second time."""
    last_error = None
    for address in addresses:
        family = socket.AF_INET if address.version == 4 else socket.AF_INET6
        sock = None
        try:
            sock = socket.socket(family, socket.SOCK_STREAM)
            # Requests has converted the explicit timeout=10 to a numeric
            # connect timeout before urllib3 creates this connection.
            sock.settimeout(cast(float, conn.timeout))
            for option in conn.socket_options or ():
                sock.setsockopt(*option)
            sock.connect((str(address), conn.port))
            return sock
        except OSError as exc:
            if sock is not None:
                sock.close()
            last_error = exc
    raise NewConnectionError(
        conn, "Unable to connect to input download destination"
    ) from last_error


def _pinned_adapter(addresses: tuple[IPAddress, ...]) -> HTTPAdapter:
    # Only the TCP socket changes. urllib3 retains the original hostname for
    # Host, SNI and certificate verification, including on connection retries.
    class PinnedHTTPConnection(HTTPConnection):
        def _new_conn(self) -> socket.socket:
            return _connect(self, addresses)

    class PinnedHTTPSConnection(HTTPSConnection):
        def _new_conn(self) -> socket.socket:
            return _connect(self, addresses)

    class PinnedHTTPPool(HTTPConnectionPool):
        ConnectionCls = PinnedHTTPConnection

    class PinnedHTTPSPool(HTTPSConnectionPool):
        ConnectionCls = PinnedHTTPSConnection

    adapter = HTTPAdapter(max_retries=0)
    # Assign a new mapping: mutating the default mapping would affect unrelated
    # Requests sessions and concurrent downloads.
    adapter.poolmanager.pool_classes_by_scheme = {
        "http": PinnedHTTPPool,
        "https": PinnedHTTPSPool,
    }
    return adapter


class _NoRedirectSession(requests.Session):
    def resolve_redirects(
        self, resp: requests.Response, req: requests.PreparedRequest, **kwargs: Any
    ) -> Iterator[requests.Response]:
        # Session.send prepares response.next even with allow_redirects=False,
        # which consumes the redirect body. Reject without reading that body.
        return iter(())


def fetch_input(url: str, headers: dict[str, str]) -> requests.Response:
    """Return a streaming response; the caller owns and must close it."""
    _validate_url(url)
    response = None
    try:
        with _NoRedirectSession() as session:
            # Neither environment proxies nor netrc may override this policy.
            session.trust_env = False
            request = session.prepare_request(
                requests.Request("GET", url, headers=headers)
            )
            assert request.url is not None
            _validate_url(request.url)
            parsed = urlsplit(request.url)
            assert parsed.hostname is not None
            port = parsed.port or (443 if parsed.scheme == "https" else 80)
            addresses = _validated_addresses(parsed.hostname, port)
            session.mount(f"{parsed.scheme}://", _pinned_adapter(addresses))
            response = session.send(
                request, stream=True, timeout=10, allow_redirects=False
            )
            if 300 <= response.status_code < 400:
                raise ValueError("Input download redirects are not allowed")
            response.raise_for_status()
            response.raw.decode_content = True
            return response
    except (ValueError, requests.RequestException):
        if response is not None:
            response.close()
        raise
