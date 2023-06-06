import os
import tempfile
import uuid

import pytest
from cog.server.helpers import StreamRedirector, WrappedStream


@pytest.fixture
def tmpdir():
    with tempfile.TemporaryDirectory() as td:
        yield td


@pytest.fixture
def tmpfile(tmpdir):
    def _tmpfile():
        return os.path.join(tmpdir, uuid.uuid4().hex)

    return _tmpfile


def test_wrapped_stream_can_read_from_wrapped(tmpfile):
    """
    WrappedStream exposes a `wrapped` file object that can be used to read data
    written to the stapped stream.
    """
    filename = tmpfile()
    fake_stream = open(filename, "w")
    ws = WrappedStream("fake", fake_stream)
    ws.wrap()

    fake_stream.write("test data\n")
    fake_stream.flush()

    assert ws.wrapped.readline() == "test data\n"


def test_wrapped_stream_writes_to_underlying_stream(tmpfile):
    """
    WrappedStream has `write()` and `flush()` methods that are passed through to the underlying stream.
    """
    filename = tmpfile()
    fake_stream = open(filename, "w")
    ws = WrappedStream("fake", fake_stream)
    ws.wrap()

    ws.write("test data\n")
    ws.flush()

    assert ws.wrapped.readline() == "test data\n"


def test_wrapped_stream_can_write_to_original(tmpfile):
    """
    WrappedStream exposes an `original` file object than be used to write data
    to the original stream destination (before it was wrapped).
    """
    filename = tmpfile()
    fake_stream = open(filename, "w")
    ws = WrappedStream("fake", fake_stream)
    ws.wrap()

    ws.original.write("test data\n")
    ws.original.flush()
    fake_stream.close()

    output = open(filename).read()

    assert output == "test data\n"


def test_wrapped_stream_writes_are_intercepted(tmpfile):
    """
    After wrapping, writes to the passed-in stream go to an internal pipe, not
    the stream.
    """
    filename = tmpfile()
    fake_stream = open(filename, "w")
    ws = WrappedStream("fake", fake_stream)
    ws.wrap()

    fake_stream.write("test data\n")
    fake_stream.flush()
    fake_stream.close()

    output = open(filename).read()

    assert output == ""


def test_wrapped_stream_raises_if_used_before_wrap(tmpfile):
    """
    You must explicitly call `.wrap()` before attempting to use the
    WrappedStream.
    """
    filename = tmpfile()
    fake_stream = open(filename, "w")
    ws = WrappedStream("fake", fake_stream)

    with pytest.raises(RuntimeError):
        ws.wrapped.readline()

    with pytest.raises(RuntimeError):
        ws.original.write("test data\n")


def test_stream_redirector(tmpfile):
    stdout_filename = tmpfile()
    stderr_filename = tmpfile()
    fake_stdout = open(stdout_filename, "w")
    fake_stderr = open(stderr_filename, "w")
    ws_stdout = WrappedStream("fake_stdout", fake_stdout)
    ws_stderr = WrappedStream("fake_stderr", fake_stderr)
    output = []

    def _write_hook(stream_name, original_stream, data):
        output.append((stream_name, data))

    ws_stdout.wrap()
    ws_stderr.wrap()
    r = StreamRedirector([ws_stdout, ws_stderr], _write_hook)
    r.start()

    fake_stdout.write("hello to stdout\n")
    fake_stdout.flush()
    fake_stderr.write("hello to stderr\n")
    fake_stderr.flush()

    r.drain()

    assert open(stdout_filename).read() == ""
    assert open(stderr_filename).read() == ""
    assert output == [
        ("fake_stdout", "hello to stdout\n"),
        ("fake_stderr", "hello to stderr\n"),
    ]

    r.shutdown()


@pytest.mark.parametrize(
    "writes,expected_output",
    [
        # lines are preserved
        (["hello world\n"], ["hello world\n"]),
        # partial lines at end are flushed (complete with a newline that wasn't
        # actually written)
        (["one\n", "two"], ["one\n", "two\n"]),
        # partial lines in the middle are buffered until a newline is seen
        (["one", "two", "three\n"], ["onetwothree\n"]),
    ],
)
def test_stream_redirector_line_handling(tmpfile, writes, expected_output):
    filename = tmpfile()
    fake_stream = open(filename, "w")
    ws = WrappedStream("fake_stream", fake_stream)
    output = []

    def _write_hook(stream_name, original_stream, data):
        output.append((stream_name, data))

    ws.wrap()
    r = StreamRedirector([ws], _write_hook)
    r.start()

    for w in writes:
        fake_stream.write(w)
        fake_stream.flush()

    r.drain()

    assert open(filename).read() == ""
    assert output == [("fake_stream", o) for o in expected_output]

    r.shutdown()


def test_stream_redirector_with_no_streams_raises():
    def _write_hook(stream_name, original_stream, data):
        pass

    with pytest.raises(ValueError):
        StreamRedirector([], _write_hook)
