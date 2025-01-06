import os
import resource
import time
import uuid

import pytest

from cog.server.helpers import StreamRedirector


@pytest.fixture
def tmpfile(tmp_path):
    def _tmpfile():
        return tmp_path / uuid.uuid4().hex

    return _tmpfile


def test_stream_redirector_multiple_streams(tmpfile):
    stdout_file = tmpfile()
    stderr_file = tmpfile()
    fake_stdout = stdout_file.open("w")
    fake_stderr = stderr_file.open("w")
    output = []

    def _write_hook(stream_name, data):
        output.append((stream_name, data))

    r = StreamRedirector(callback=_write_hook, streams=[fake_stdout, fake_stderr])

    with r:
        fake_stdout.write("hello to stdout\n")
        fake_stdout.flush()
        fake_stderr.write("hello to stderr\n")
        fake_stderr.flush()

        r.drain()

    assert stdout_file.read_text() == ""
    assert stderr_file.read_text() == ""
    assert output == [
        (stdout_file.as_posix(), "hello to stdout\n"),
        (stderr_file.as_posix(), "hello to stderr\n"),
    ]


def test_stream_redirector_bench():
    fake_stdout = open(os.devnull, "w")

    def _write_hook(stream_name, data):
        pass

    r = StreamRedirector(callback=_write_hook, streams=[fake_stdout])

    n = 1024
    i = 0
    now = time.monotonic()

    with r:
        while time.monotonic() - now < 1:
            fake_stdout.write("0" * (n - 1) + "\n")
            fake_stdout.flush()
            i += 1
        r.drain()

    delta = time.monotonic() - now

    print(
        f"wrote {i} lines in {delta:.4f} seconds ({i / delta:.2f} lines/sec, {i * n / delta:.2f} bytes/sec)"
    )
    # We should be able to push at least 100MB/s through the stream redirector.
    #
    # For reasons we're not yet sure of, throughput on GitHub Actions is really
    # poor, so we're setting the threshold to 25MB/s for now.
    assert i * n / delta > 25e6

    fake_stdout.close()


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
    f = tmpfile()
    stream = f.open("w")
    output = []

    def _write_hook(stream_name, data):
        output.append((stream_name, data))

    r = StreamRedirector(callback=_write_hook, streams=[stream])

    with r:
        for w in writes:
            stream.write(w)
            stream.flush()

        r.drain()

    stream.write("no longer redirected\n")
    stream.flush()

    assert f.read_text() == "no longer redirected\n"
    assert output == [(f.as_posix(), o) for o in expected_output]

    stream.close()


def test_stream_redirector_reentrant(tmpfile):
    f = tmpfile()
    stream = f.open("w")
    output = []

    def _write_hook(stream_name, data):
        output.append((stream_name, data))

    r = StreamRedirector(callback=_write_hook, streams=[stream])

    with r:
        stream.write("one\n")
        stream.flush()

        with r:
            stream.write("two\n")
            stream.flush()

            with r:
                stream.write("three\n")
                stream.flush()

        r.drain()

    stream.write("four\n")
    stream.flush()
    stream.close()

    assert f.read_text() == "four\n"
    assert output == [
        (f.as_posix(), "one\n"),
        (f.as_posix(), "two\n"),
        (f.as_posix(), "three\n"),
    ]


def test_stream_redirector_tee(tmpfile):
    f = tmpfile()
    stream = f.open("w")
    output = []

    def _write_hook(stream_name, data):
        output.append((stream_name, data))

    r = StreamRedirector(callback=_write_hook, tee=True, streams=[stream])

    with r:
        stream.write("one\n")
        stream.write("two\n")
        stream.write("three\n")
        stream.flush()

        r.drain()

    stream.write("four\n")
    stream.flush()
    stream.close()

    assert f.read_text() == "one\ntwo\nthree\nfour\n"
    assert output == [
        (f.as_posix(), "one\n"),
        (f.as_posix(), "two\n"),
        (f.as_posix(), "three\n"),
    ]


def test_stream_redirector_does_not_leak_file_descriptors(tmpfile, request):
    f = tmpfile()
    stream = f.open("w")

    def _write_hook(stream_name, data):
        pass

    original_limits = resource.getrlimit(resource.RLIMIT_NOFILE)
    resource.setrlimit(resource.RLIMIT_NOFILE, (256, original_limits[1]))
    request.addfinalizer(
        lambda: resource.setrlimit(resource.RLIMIT_NOFILE, original_limits)
    )

    r = StreamRedirector(callback=_write_hook, streams=[stream])

    for _ in range(10 * 256):
        with r:
            stream.write("one\n")
            stream.flush()
            r.drain()

    stream.close()
