import io
import pickle

import responses
from cog.types import URLFile


@responses.activate
def test_urlfile_acts_like_response():
    responses.get(
        "https://example.com/some/url",
        json={"message": "hello world"},
        status=200,
    )

    u = URLFile("https://example.com/some/url")

    assert isinstance(u, io.IOBase)
    assert u.read() == b'{"message": "hello world"}'


@responses.activate
def test_urlfile_iterable():
    responses.get(
        "https://example.com/some/url",
        body="one\ntwo\nthree\n",
        status=200,
    )

    u = URLFile("https://example.com/some/url")
    result = list(u)

    assert result == [b"one\n", b"two\n", b"three\n"]


@responses.activate
def test_urlfile_no_request_if_not_used():
    # This test would be failed by responses if the request were actually made,
    # as we've not registered the handler for it.
    URLFile("https://example.com/some/url")


@responses.activate
def test_urlfile_can_be_pickled():
    u = URLFile("https://example.com/some/url")

    result = pickle.loads(pickle.dumps(u))

    assert isinstance(result, URLFile)


@responses.activate
def test_urlfile_can_be_pickled_even_once_loaded():
    responses.get(
        "https://example.com/some/url",
        json={"message": "hello world"},
        status=200,
    )

    u = URLFile("https://example.com/some/url")
    u.read()

    result = pickle.loads(pickle.dumps(u))

    assert isinstance(result, URLFile)
