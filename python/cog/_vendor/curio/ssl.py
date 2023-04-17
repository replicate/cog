# curio/ssl.py
#
# Wrapper around built-in SSL module

__all__ = []

# -- Standard Library

from functools import wraps, partial

try:
    import ssl as _ssl
    from ssl import *
except ImportError:
    _ssl = None

    # We need these exceptions defined, even if ssl is not available.
    class SSLWantReadError(Exception):
        pass

    class SSLWantWriteError(Exception):
        pass

# -- Curio

from .workers import run_in_thread
from .io import Socket

if _ssl:
    @wraps(_ssl.wrap_socket)
    async def wrap_socket(sock, *args, do_handshake_on_connect=True, **kwargs):
        if isinstance(sock, Socket):
            sock = sock._socket

        ssl_sock = _ssl.wrap_socket(sock, *args, do_handshake_on_connect=False, **kwargs)
        cssl_sock = Socket(ssl_sock)
        cssl_sock.do_handshake_on_connect = do_handshake_on_connect
        if do_handshake_on_connect and ssl_sock._connected:
            await cssl_sock.do_handshake()
        return cssl_sock

    @wraps(_ssl.get_server_certificate)
    async def get_server_certificate(*args, **kwargs):
        return await run_in_thread(partial(_ssl.get_server_certificate, *args, **kwargs))

    # Small wrapper class to make sure the wrap_socket() method returns the right type
    class CurioSSLContext(object):

        def __init__(self, context):
            self._context = context

        def __getattr__(self, name):
            return getattr(self._context, name)

        async def wrap_socket(self, sock, *args, do_handshake_on_connect=True, **kwargs):
            sock = self._context.wrap_socket(
                sock._socket, *args, do_handshake_on_connect=False, **kwargs)
            csock = Socket(sock)
            csock.do_handshake_on_connect = do_handshake_on_connect
            if do_handshake_on_connect and sock._connected:
                await csock.do_handshake()
            return csock

        def __setattr__(self, name, value):
            if name == '_context':
                super().__setattr__(name, value)
            else:
                setattr(self._context, name, value)

    # Name alias
    def SSLContext(protocol):
        return CurioSSLContext(_ssl.SSLContext(protocol))

    @wraps(_ssl.create_default_context)
    def create_default_context(*args, **kwargs):
        context = _ssl.create_default_context(*args, **kwargs)
        return CurioSSLContext(context)
