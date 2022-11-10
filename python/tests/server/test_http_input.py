import base64
import os
import tempfile

import responses
from PIL import Image
from pydantic import BaseModel

from cog import BasePredictor, File, Input, Path

from .test_http import make_client


def test_no_input():
    class Predictor(BasePredictor):
        def predict(self) -> str:
            return "foobar"

    client = make_client(Predictor())
    resp = client.post("/predictions")
    assert resp.status_code == 200
    assert resp.json() == {"status": "succeeded", "output": "foobar"}


def test_good_str_input():
    class Predictor(BasePredictor):
        def predict(self, text: str) -> str:
            return text

    client = make_client(Predictor())
    resp = client.post("/predictions", json={"input": {"text": "baz"}})
    assert resp.status_code == 200
    assert resp.json() == {"status": "succeeded", "output": "baz"}


def test_good_int_input():
    class Predictor(BasePredictor):
        def predict(self, num: int) -> int:
            return num**3

    client = make_client(Predictor())
    resp = client.post("/predictions", json={"input": {"num": 3}})
    assert resp.status_code == 200
    assert resp.json() == {"output": 27, "status": "succeeded"}
    resp = client.post("/predictions", json={"input": {"num": -3}})
    assert resp.status_code == 200
    assert resp.json() == {"output": -27, "status": "succeeded"}


def test_bad_int_input():
    class Predictor(BasePredictor):
        def predict(self, num: int) -> int:
            return num**2

    client = make_client(Predictor())
    resp = client.post("/predictions", json={"input": {"num": "foo"}})
    assert resp.json() == {
        "detail": [
            {
                "loc": ["body", "input", "num"],
                "msg": "value is not a valid integer",
                "type": "type_error.integer",
            }
        ]
    }
    assert resp.status_code == 422


def test_default_int_input():
    class Predictor(BasePredictor):
        def predict(self, num: int = Input(default=5)) -> int:
            return num**2

    client = make_client(Predictor())

    resp = client.post("/predictions", json={"input": {}})
    assert resp.status_code == 200
    assert resp.json() == {"output": 25, "status": "succeeded"}

    resp = client.post("/predictions", json={"input": {"num": 3}})
    assert resp.status_code == 200
    assert resp.json() == {"output": 9, "status": "succeeded"}


def test_file_input_data_url():
    class Predictor(BasePredictor):
        def predict(self, file: File) -> str:
            return file.read()

    client = make_client(Predictor())
    resp = client.post(
        "/predictions",
        json={
            "input": {
                "file": "data:text/plain;base64,"
                + base64.b64encode(b"bar").decode("utf-8")
            }
        },
    )
    assert resp.json() == {"output": "bar", "status": "succeeded"}
    assert resp.status_code == 200


@responses.activate
def test_file_input_with_http_url():
    class Predictor(BasePredictor):
        def predict(self, file: File) -> str:
            return file.read()

    responses.add(responses.GET, "http://example.com/foo.txt", body="hello")

    client = make_client(Predictor())
    resp = client.post(
        "/predictions",
        json={"input": {"file": "http://example.com/foo.txt"}},
    )
    assert resp.json() == {"output": "hello", "status": "succeeded"}


def test_path_input_data_url():
    class Predictor(BasePredictor):
        def predict(self, path: Path) -> str:
            with open(path) as fh:
                extension = fh.name.split(".")[-1]
                return f"{extension} {fh.read()}"

    client = make_client(Predictor())
    resp = client.post(
        "/predictions",
        json={
            "input": {
                "path": "data:text/plain;base64,"
                + base64.b64encode(b"bar").decode("utf-8")
            }
        },
    )
    assert resp.json() == {"output": "txt bar", "status": "succeeded"}
    assert resp.status_code == 200


def test_path_temporary_files_are_removed():
    class Predictor(BasePredictor):
        def predict(self, path: Path) -> str:
            return str(path)

    client = make_client(Predictor())
    resp = client.post(
        "/predictions",
        json={
            "input": {
                "path": "data:text/plain;base64,"
                + base64.b64encode(b"bar").decode("utf-8")
            }
        },
    )
    temporary_path = resp.json()["output"]
    assert not os.path.exists(temporary_path)


@responses.activate
def test_file_input_with_http_url():
    class Predictor(BasePredictor):
        def predict(self, path: Path) -> str:
            with open(path) as fh:
                extension = fh.name.split(".")[-1]
                return f"{extension} {fh.read()}"

    responses.add(responses.GET, "http://example.com/foo.txt", body="hello")

    client = make_client(Predictor())
    resp = client.post(
        "/predictions",
        json={"input": {"path": "http://example.com/foo.txt"}},
    )
    assert resp.json() == {"output": "txt hello", "status": "succeeded"}


def test_file_bad_input():
    class Predictor(BasePredictor):
        def predict(self, file: File) -> str:
            return file.read()

    client = make_client(Predictor())
    resp = client.post(
        "/predictions",
        json={"input": {"file": "foo"}},
    )
    assert resp.status_code == 422


def test_path_output_file():
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


def test_multiple_arguments():
    class Predictor(BasePredictor):
        def predict(
            self,
            text: str,
            path: Path,
            num1: int,
            num2: int = Input(default=10),
        ) -> str:
            with open(path) as fh:
                return text + " " + str(num1 * num2) + " " + fh.read()

    client = make_client(Predictor())
    resp = client.post(
        "/predictions",
        json={
            "input": {
                "text": "baz",
                "num1": 5,
                "path": "data:text/plain;base64,"
                + base64.b64encode(b"wibble").decode("utf-8"),
            }
        },
    )
    assert resp.status_code == 200
    assert resp.json() == {"output": "baz 50 wibble", "status": "succeeded"}


def test_gt_lt():
    class Predictor(BasePredictor):
        def predict(self, num: float = Input(ge=3.01, le=10.5)) -> float:
            return num

    client = make_client(Predictor())
    resp = client.post("/predictions", json={"input": {"num": 2}})
    assert resp.json() == {
        "detail": [
            {
                "ctx": {"limit_value": 3.01},
                "loc": ["body", "input", "num"],
                "msg": "ensure this value is greater than or equal to 3.01",
                "type": "value_error.number.not_ge",
            }
        ]
    }
    assert resp.status_code == 422

    resp = client.post("/predictions", json={"input": {"num": 5}})
    assert resp.status_code == 200


def test_choices_str():
    class Predictor(BasePredictor):
        def predict(self, text: str = Input(choices=["foo", "bar"])) -> str:
            assert type(text) == str
            return text

    client = make_client(Predictor())
    resp = client.post("/predictions", json={"input": {"text": "foo"}})
    assert resp.status_code == 200
    resp = client.post("/predictions", json={"input": {"text": "baz"}})
    assert resp.status_code == 422


def test_choices_int():
    class Predictor(BasePredictor):
        def predict(self, x: int = Input(choices=[1, 2])) -> int:
            return x**2

    client = make_client(Predictor())
    resp = client.post("/predictions", json={"input": {"x": 1}})
    assert resp.status_code == 200
    resp = client.post("/predictions", json={"input": {"x": 3}})
    assert resp.status_code == 422
