import os
import tempfile

import pytest

from cog.server.helpers import WrappedStream


@pytest.fixture
def tmpdir():
    with tempfile.TemporaryDirectory() as td:
        yield td


@pytest.fixture
def tmpname(tmpdir):
    return os.path.join(tmpdir, 'fake-stream')


def test_wrapped_stream_can_read_from_wrapped(tmpname):
    """
    WrappedStream exposes a `wrapped` file object that can be used to read data
    written to the stapped stream.
    """
    fake_stream = open(tmpname, "w")
    ws = WrappedStream("fake", fake_stream)
    ws.wrap()

    fake_stream.write("test data\n")
    fake_stream.flush()

    assert ws.wrapped.readline() == "test data\n"


def test_wrapped_stream_can_write_to_original(tmpname):
    """
    WrappedStream exposes an `original` file object than be used to write data
    to the original stream destination (before it was wrapped).
    """
    fake_stream = open(tmpname, "w")
    ws = WrappedStream("fake", fake_stream)
    ws.wrap()

    ws.original.write("test data\n")
    ws.original.flush()
    fake_stream.close()

    output = open(tmpname, "r").read()

    assert output == "test data\n"


def test_wrapped_stream_writes_are_intercepted(tmpname):
    """
    After wrapping, writes to the passed-in stream go to an internal pipe, not
    the stream.
    """
    fake_stream = open(tmpname, "w")
    ws = WrappedStream("fake", fake_stream)
    ws.wrap()

    fake_stream.write("test data\n")
    fake_stream.flush()
    fake_stream.close()

    output = open(tmpname, "r").read()

    assert output == ""


def test_wrapped_stream_raises_if_used_before_wrap(tmpname):
    """
    You must explicitly call `.wrap()` before attempting to use the
    WrappedStream.
    """
    fake_stream = open(tmpname, "w")
    ws = WrappedStream("fake", fake_stream)

    with pytest.raises(RuntimeError):
        ws.wrapped.readline()

    with pytest.raises(RuntimeError):
        ws.original.write("test data\n")
