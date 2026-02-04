"""Tests for cog.types module."""

import io
from dataclasses import is_dataclass

from cog import (
    Path,
    Secret,
    File,
    URLFile,
    ConcatenateIterator,
    AsyncConcatenateIterator,
)


class TestSecret:
    """Tests for Secret type."""

    def test_secret_creation(self) -> None:
        secret = Secret(secret_value="my-api-key")
        assert secret.get_secret_value() == "my-api-key"

    def test_secret_masks_in_str(self) -> None:
        secret = Secret(secret_value="my-api-key")
        assert str(secret) == "**********"
        assert "my-api-key" not in str(secret)

    def test_secret_masks_in_repr(self) -> None:
        secret = Secret(secret_value="my-api-key")
        assert "my-api-key" not in repr(secret)
        assert "**********" in repr(secret)

    def test_secret_none_value(self) -> None:
        secret = Secret(secret_value=None)
        assert secret.get_secret_value() is None
        assert str(secret) == ""

    def test_secret_default_none(self) -> None:
        secret = Secret()
        assert secret.get_secret_value() is None

    def test_secret_is_dataclass(self) -> None:
        assert is_dataclass(Secret)

    def test_secret_is_frozen(self) -> None:
        secret = Secret(secret_value="test")
        try:
            secret.secret_value = "new"  # type: ignore[misc]
            assert False, "Should have raised FrozenInstanceError"
        except Exception:
            pass  # Expected - frozen dataclass


class TestPath:
    """Tests for Path type."""

    def test_path_from_string(self) -> None:
        p = Path("/tmp/test.txt")
        assert str(p) == "/tmp/test.txt"

    def test_path_is_pathlib_subclass(self) -> None:
        import pathlib

        p = Path("/tmp/test.txt")
        assert isinstance(p, pathlib.PosixPath)


class TestFile:
    """Tests for File type (deprecated)."""

    def test_file_validate_iobase(self) -> None:
        buf = io.BytesIO(b"test data")
        result = File.validate(buf)
        assert result is buf

    def test_file_validate_data_uri(self) -> None:
        # data URI with plain text
        data_uri = "data:text/plain;base64,SGVsbG8gV29ybGQ="
        result = File.validate(data_uri)
        assert isinstance(result, io.BytesIO)
        assert result.read() == b"Hello World"

    def test_file_validate_invalid_scheme(self) -> None:
        try:
            File.validate("ftp://example.com/file.txt")
            assert False, "Should have raised ValueError"
        except ValueError as e:
            assert "not a valid URL scheme" in str(e)


class TestURLFile:
    """Tests for URLFile type."""

    def test_urlfile_creation(self) -> None:
        url = "https://example.com/image.jpg"
        uf = URLFile(url)
        assert uf.name == "image.jpg"

    def test_urlfile_invalid_scheme(self) -> None:
        try:
            URLFile("ftp://example.com/file.txt")
            assert False, "Should have raised ValueError"
        except ValueError as e:
            assert "HTTP or HTTPS" in str(e)

    def test_urlfile_picklable(self) -> None:
        import pickle

        url = "https://example.com/image.jpg"
        uf = URLFile(url)
        pickled = pickle.dumps(uf)
        restored = pickle.loads(pickled)
        assert restored.name == "image.jpg"

    def test_urlfile_custom_filename(self) -> None:
        url = "https://example.com/image.jpg"
        uf = URLFile(url, filename="custom.png")
        assert uf.name == "custom.png"


class TestIterators:
    """Tests for iterator types."""

    def test_concatenate_iterator_is_abstract(self) -> None:
        # ConcatenateIterator should be usable as a type hint
        from typing import Iterator

        assert issubclass(ConcatenateIterator, Iterator)

    def test_async_concatenate_iterator_is_abstract(self) -> None:
        # AsyncConcatenateIterator should be usable as a type hint
        from typing import AsyncIterator

        assert issubclass(AsyncConcatenateIterator, AsyncIterator)
