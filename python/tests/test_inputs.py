import io
import os
from pathlib import Path

import cog
from .client import make_client


def test_no_input():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "foo"

        def predict(self):
            return self.foo + "bar"

    client = make_client(Predictor())
    resp = client.post("/predict")
    assert resp.status_code == 200
    assert resp.data == b"foobar"


def test_good_str_input():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "foo"

        @cog.input("text", type=str)
        def predict(self, text):
            return self.foo + text

    client = make_client(Predictor())
    resp = client.post("/predict", data={"text": "baz"})
    assert resp.status_code == 200
    assert resp.data == b"foobaz"


def test_good_int_input():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "foo"

        @cog.input("num", type=int)
        def predict(self, num):
            num2 = num ** 3
            return self.foo + str(num2)

    client = make_client(Predictor())
    resp = client.post("/predict", data={"num": 3})
    assert resp.status_code == 200
    assert resp.data == b"foo27"
    resp = client.post("/predict", data={"num": -3})
    assert resp.status_code == 200
    assert resp.data == b"foo-27"


def test_bad_int_input():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "foo"

        @cog.input("num", type=int)
        def predict(self, num):
            num2 = num ** 2
            return self.foo + str(num2)

    client = make_client(Predictor())
    resp = client.post("/predict", data={"num": "foo"})
    assert resp.status_code == 400


def test_default_int_input():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "foo"

        @cog.input("num", type=int, default=5)
        def predict(self, num):
            num2 = num ** 2
            return self.foo + str(num2)

    client = make_client(Predictor())
    resp = client.post("/predict", data={"num": 3})
    assert resp.status_code == 200
    assert resp.data == b"foo9"
    resp = client.post("/predict")
    assert resp.status_code == 200
    assert resp.data == b"foo25"

def test_good_float_input():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "foo"

        @cog.input("num", type=float)
        def predict(self, num):
            num2 = num ** 3
            return self.foo + str(num2)

    client = make_client(Predictor())
    resp = client.post("/predict", data={"num": 3})
    assert resp.status_code == 200
    assert resp.data == b"foo27.0"
    resp = client.post("/predict", data={"num": 3.5})
    assert resp.status_code == 200
    assert resp.data == b"foo42.875"
    resp = client.post("/predict", data={"num": -3.5})
    assert resp.status_code == 200
    assert resp.data == b"foo-42.875"

def test_bad_float_input():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "foo"

        @cog.input("num", type=float)
        def predict(self, num):
            num2 = num ** 2
            return self.foo + str(num2)

    client = make_client(Predictor())
    resp = client.post("/predict", data={"num": "foo"})
    assert resp.status_code == 400


def test_good_bool_input():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "foo"

        @cog.input("flag", type=bool)
        def predict(self, flag):
            if flag:
                return self.foo + "yes"
            else:
                return self.foo + "no"

    client = make_client(Predictor())
    resp = client.post("/predict", data={"flag": True})
    assert resp.status_code == 200
    assert resp.data == b"fooyes"
    resp = client.post("/predict", data={"flag": False})
    assert resp.status_code == 200
    assert resp.data == b"foono"



def test_good_path_input():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "foo"

        @cog.input("path", type=Path)
        def predict(self, path):
            with open(path) as f:
                return self.foo + " " + f.read() + " " + os.path.basename(path)

    client = make_client(Predictor())
    path_data = (io.BytesIO(b"bar"), "foo.txt")
    resp = client.post(
        "/predict", data={"path": path_data}, content_type="multipart/form-data"
    )
    assert resp.status_code == 200
    assert resp.data == b"foo bar foo.txt"


def test_bad_path_input():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "foo"

        @cog.input("path", type=Path)
        def predict(self, path):
            with open(path) as f:
                return self.foo + " " + f.read() + " " + os.path.basename(path)

    client = make_client(Predictor())
    resp = client.post("/predict", data={"path": "bar"})
    assert resp.status_code == 400


def test_default_path_input():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "foo"

        @cog.input("path", type=Path, default=None)
        def predict(self, path):
            if path is None:
                return "noneee"
            with open(path) as f:
                return self.foo + " " + f.read() + " " + os.path.basename(path)

    client = make_client(Predictor())
    path_data = (io.BytesIO(b"bar"), "foo.txt")
    resp = client.post(
        "/predict", data={"path": path_data}, content_type="multipart/form-data"
    )
    assert resp.status_code == 200
    assert resp.data == b"foo bar foo.txt"
    resp = client.post("/predict", data={})
    assert resp.status_code == 200
    assert resp.data == b"noneee"
