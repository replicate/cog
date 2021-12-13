import io
from pathlib import Path
import unittest.mock

import pytest

import cog
from .client import make_client


@pytest.mark.skip("This should work but doesn't at the moment")
def test_bad_input_name():
    with pytest.raises(TypeError):

        class Predictor(cog.Predictor):
            def setup(self):
                self.foo = "foo"

            @cog.input("text", type=str)
            def predict(self, bad):
                return self.foo + "bar"



def test_extranous_input_keys():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "foo"

        @cog.input("text", type=str)
        def predict(self, text):
            return self.foo + text

    client = make_client(Predictor())
    resp = client.post("/predict", data={"text": "baz", "text2": "qux"})
    assert resp.status_code == 400


def test_min_max():
    class Predictor(cog.Predictor):
        def setup(self):
            pass

        @cog.input("num1", type=float, min=3, max=10.5)
        @cog.input("num2", type=float, min=-4)
        @cog.input("num3", type=int, max=-4)
        def predict(self, num1, num2, num3):
            return num1 + num2 + num3

    client = make_client(Predictor())
    resp = client.post("/predict", data={"num1": 3, "num2": -4, "num3": -4})
    assert resp.status_code == 200
    assert resp.data == b"-5.0"
    resp = client.post("/predict", data={"num1": 2, "num2": -4, "num3": -4})
    assert resp.status_code == 400
    resp = client.post("/predict", data={"num1": 3, "num2": -4.1, "num3": -4})
    assert resp.status_code == 400
    resp = client.post("/predict", data={"num1": 3, "num2": -4, "num3": -3})
    assert resp.status_code == 400


def test_good_options():
    class Predictor(cog.Predictor):
        def setup(self):
            pass

        @cog.input("text", type=str, options=["foo", "bar"])
        @cog.input("num", type=int, options=[1, 2, 3])
        def predict(self, text, num):
            return text + ("a" * num)

    client = make_client(Predictor())
    resp = client.post("/predict", data={"text": "foo", "num": 2})
    assert resp.status_code == 200
    assert resp.data == b"fooaa"


def test_bad_options():
    class Predictor(cog.Predictor):
        def setup(self):
            pass

        @cog.input("text", type=str, options=["foo", "bar"])
        @cog.input("num", type=int, options=[1, 2, 3])
        def predict(self, text, num):
            return text + ("a" * num)

    client = make_client(Predictor())
    resp = client.post("/predict", data={"text": "baz", "num": 2})
    assert resp.status_code == 400
    resp = client.post("/predict", data={"text": "bar", "num": 4})
    assert resp.status_code == 400

def test_bad_options_type():
    with pytest.raises(ValueError):

        class Predictor(cog.Predictor):
            def setup(self):
                pass

            @cog.input("text", type=str, options=[])
            def predict(self, text):
                return text

    with pytest.raises(ValueError):

        class Predictor(cog.Predictor):
            def setup(self):
                pass

            @cog.input("text", type=str, options=["foo"])
            def predict(self, text):
                return text

    with pytest.raises(ValueError):

        class Predictor(cog.Predictor):
            def setup(self):
                pass

            @cog.input("text", type=Path, options=["foo"])
            def predict(self, text):
                return text

def test_multiple_arguments():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "foo"

        @cog.input("text", type=str)
        @cog.input("num1", type=int)
        @cog.input("num2", type=int, default=10)
        @cog.input("path", type=Path)
        def predict(self, text, num1, num2, path):
            with open(path) as f:
                path_contents = f.read()
            return self.foo + " " + text + " " + str(num1 * num2) + " " + path_contents

    client = make_client(Predictor())
    path_data = (io.BytesIO(b"bar"), "foo.txt")
    resp = client.post(
        "/predict",
        data={"text": "baz", "num1": 5, "path": path_data},
        content_type="multipart/form-data",
    )
    assert resp.status_code == 200
    assert resp.data == b"foo baz 50 bar"


@unittest.mock.patch("time.time", return_value=0.0)
def test_timing(time_mock):
    class Predictor(cog.Predictor):
        def setup(self):
            time_mock.return_value = 1.0

        def predict(self):
            time_mock.return_value = 3.0
            return ""

    client = make_client(Predictor())
    resp = client.post("/predict")
    assert resp.status_code == 200
    assert float(resp.headers["X-Setup-Time"]) == 1.0
    assert float(resp.headers["X-Run-Time"]) == 2.0
