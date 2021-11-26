import io
import mimetypes
import os
import base64
import pathlib
import shutil
import tempfile
from typing import Any, BinaryIO
from urllib.parse import urlparse


def get_filename(url):
    parsed_url = urlparse(url)
    if parsed_url.scheme == "data":
        header, _ = parsed_url.path.split(",", 1)
        mime_type, _ = header.split(";", 1)
        return "file" + mimetypes.guess_extension(mime_type)
    return os.path.basename(parsed_url.path)


class File(BinaryIO):
    validate_always = True

    @classmethod
    def __get_validators__(cls):
        yield cls.validate

    @classmethod
    def validate(cls, value: Any) -> BinaryIO:
        if isinstance(value, pathlib.Path):
            return value

        parsed_url = urlparse(value)
        if parsed_url.scheme == "data":
            # https://developer.mozilla.org/en-US/docs/Web/HTTP/Basics_of_HTTP/Data_URIs
            # TODO: decode properly. this maybe? https://github.com/fcurella/python-datauri/
            header, encoded = parsed_url.path.split(",", 1)
            return io.BytesIO(base64.b64decode(encoded))
        else:
            raise ValueError(
                f"'{parsed_url.scheme}' is not a valid URL scheme. 'data', ... is supported."
            )

    @classmethod
    def encode(cls, file) -> str:
        pass


class Path(pathlib.PosixPath):
    validate_always = True

    @classmethod
    def __get_validators__(cls):
        yield cls.validate

    @classmethod
    def validate(cls, value: Any) -> pathlib.Path:
        if isinstance(value, pathlib.Path):
            return value

        src = File.validate(value)
        # TODO: cleanup!
        temp_dir = tempfile.mkdtemp()
        temp_path = os.path.join(temp_dir, get_filename(value))
        with open(temp_path, "wb") as dest:
            shutil.copyfileobj(src, dest)

        return cls(dest.name)

    @classmethod
    def encode(cls, path) -> str:
        with open(path, "rb") as fh:
            encoded_body = base64.b64encode(fh.read())
            mime_type = mimetypes.guess_type(path)[0]
            s = encoded_body.decode("utf-8")
            return f"data:{mime_type};base64,{s}"
