import base64
import io
import os
import tempfile
from typing import Iterator, List

import numpy as np
import responses
from PIL import Image
from responses.matchers import multipart_matcher

from cog import BaseModel, BasePredictor, File, Path

from .test_http import make_client


def test_return_wrong_type():
    class Predictor(BasePredictor):
        def predict(self) -> int:
            return "foo"

    client = make_client(Predictor(), raise_server_exceptions=False)
    resp = client.post("/predictions")
    assert resp.status_code == 500


def test_path_output_path():
    class Predictor(BasePredictor):
        def predict(self) -> Path:
            temp_dir = tempfile.mkdtemp()
            temp_path = os.path.join(temp_dir, "my_file.bmp")
            img = Image.new("RGB", (255, 255), "red")
            img.save(temp_path)
            return Path(temp_path)

    client = make_client(Predictor())
    res = client.post("/predictions")
    assert res.status_code == 200
    header, b64data = res.json()["output"].split(",", 1)
    # need both image/bmp and image/x-ms-bmp until https://bugs.python.org/issue44211 is fixed
    assert header in ["data:image/bmp;base64", "data:image/x-ms-bmp;base64"]
    assert len(base64.b64decode(b64data)) == 195894


@responses.activate
def test_output_path_to_http():
    class Predictor(BasePredictor):
        def predict(self) -> Path:
            temp_dir = tempfile.mkdtemp()
            temp_path = os.path.join(temp_dir, "file.txt")
            with open(temp_path, "w") as fh:
                fh.write("hello")
            return Path(temp_path)

    fh = io.BytesIO(b"hello")
    fh.name = "file.txt"
    responses.add(
        responses.PUT,
        "http://example.com/upload/file.txt",
        status=201,
        match=[multipart_matcher({"file": fh})],
    )

    client = make_client(Predictor())
    res = client.post(
        "/predictions", json={"output_file_prefix": "http://example.com/upload/"}
    )
    assert res.json() == {
        "status": "succeeded",
        "output": "http://example.com/upload/file.txt",
    }
    assert res.status_code == 200


def test_path_output_file():
    class Predictor(BasePredictor):
        def predict(self) -> File:
            return io.StringIO("hello")

    client = make_client(Predictor())
    res = client.post("/predictions")
    assert res.status_code == 200
    assert res.json() == {
        "status": "succeeded",
        "output": "data:application/octet-stream;base64,aGVsbG8=",  # hello
    }


@responses.activate
def test_output_file_to_http():
    class Predictor(BasePredictor):
        def predict(self) -> File:
            fh = io.StringIO("hello")
            fh.name = "foo.txt"
            return fh

    responses.add(
        responses.PUT,
        "http://example.com/upload/foo.txt",
        status=201,
        match=[multipart_matcher({"file": ("foo.txt", b"hello")})],
    )

    client = make_client(Predictor())
    res = client.post(
        "/predictions", json={"output_file_prefix": "http://example.com/upload/"}
    )
    assert res.json() == {
        "status": "succeeded",
        "output": "http://example.com/upload/foo.txt",
    }
    assert res.status_code == 200


def test_json_output_numpy():
    class Predictor(BasePredictor):
        def predict(self) -> np.float64:
            return np.float64(1.0)

    client = make_client(Predictor())
    resp = client.post("/predictions")
    assert resp.status_code == 200
    assert resp.json() == {"output": 1.0, "status": "succeeded"}


def test_complex_output():
    class Output(BaseModel):
        text: str
        file: File

    class Predictor(BasePredictor):
        def predict(self) -> Output:
            return Output(text="hello", file=io.StringIO("hello"))

    client = make_client(Predictor())
    resp = client.post("/predictions")
    assert resp.json() == {
        "output": {
            "file": "data:application/octet-stream;base64,aGVsbG8=",
            "text": "hello",
        },
        "status": "succeeded",
    }
    assert resp.status_code == 200


def test_iterator_of_list_of_complex_output():
    class Output(BaseModel):
        text: str

    class Predictor(BasePredictor):
        def predict(self) -> Iterator[List[Output]]:
            yield [Output(text="hello")]

    client = make_client(Predictor())
    resp = client.post("/predictions")
    assert resp.json() == {
        "output": [[{"text": "hello"}]],
        "status": "succeeded",
    }
    assert resp.status_code == 200
