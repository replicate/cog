# pytest cog_test.py

import tempfile
import io
import os
from pathlib import Path

import pytest
from flask.testing import FlaskClient
from PIL import Image

import cog


def make_client(model) -> FlaskClient:
    app = model.make_app()
    app.config["TESTING"] = True
    with app.test_client() as client:
        return client


def test_no_input():
    class Model(cog.Model):
        def setup(self):
            self.foo = "foo"

        def run(self):
            return self.foo + "bar"

    client = make_client(Model())
    resp = client.post("/infer")
    assert resp.status_code == 200
    assert resp.data == b"foobar"


def test_good_str_input():
    class Model(cog.Model):
        def setup(self):
            self.foo = "foo"

        @cog.input("text", type=str)
        def run(self, text):
            return self.foo + text

    client = make_client(Model())
    resp = client.post("/infer", data={"text": "baz"})
    assert resp.status_code == 200
    assert resp.data == b"foobaz"


def test_extranous_input_keys():
    class Model(cog.Model):
        def setup(self):
            self.foo = "foo"

        @cog.input("text", type=str)
        def run(self, text):
            return self.foo + text

    client = make_client(Model())
    resp = client.post("/infer", data={"text": "baz", "text2": "qux"})
    assert resp.status_code == 400


@pytest.mark.skip("This should work but doesn't at the moment")
def test_bad_input_name():
    with pytest.raises(TypeError):

        class Model(cog.Model):
            def setup(self):
                self.foo = "foo"

            @cog.input("text", type=str)
            def run(self, bad):
                return self.foo + "bar"


def test_good_int_input():
    class Model(cog.Model):
        def setup(self):
            self.foo = "foo"

        @cog.input("num", type=int)
        def run(self, num):
            num2 = num ** 2
            return self.foo + str(num2)

    client = make_client(Model())
    resp = client.post("/infer", data={"num": 3})
    assert resp.status_code == 200
    assert resp.data == b"foo9"


def test_bad_int_input():
    class Model(cog.Model):
        def setup(self):
            self.foo = "foo"

        @cog.input("num", type=int)
        def run(self, num):
            num2 = num ** 2
            return self.foo + str(num2)

    client = make_client(Model())
    resp = client.post("/infer", data={"num": "foo"})
    assert resp.status_code == 400


def test_default_int_input():
    class Model(cog.Model):
        def setup(self):
            self.foo = "foo"

        @cog.input("num", type=int, default=5)
        def run(self, num):
            num2 = num ** 2
            return self.foo + str(num2)

    client = make_client(Model())
    resp = client.post("/infer", data={"num": 3})
    assert resp.status_code == 200
    assert resp.data == b"foo9"
    resp = client.post("/infer")
    assert resp.status_code == 200
    assert resp.data == b"foo25"


def test_good_path_input():
    class Model(cog.Model):
        def setup(self):
            self.foo = "foo"

        @cog.input("path", type=Path)
        def run(self, path):
            with open(path) as f:
                return self.foo + " " + f.read() + " " + os.path.basename(path)

    client = make_client(Model())
    path_data = (io.BytesIO(b"bar"), "foo.txt")
    resp = client.post(
        "/infer", data={"path": path_data}, content_type="multipart/form-data"
    )
    assert resp.status_code == 200
    assert resp.data == b"foo bar foo.txt"


def test_bad_path_input():
    class Model(cog.Model):
        def setup(self):
            self.foo = "foo"

        @cog.input("path", type=Path)
        def run(self, path):
            with open(path) as f:
                return self.foo + " " + f.read() + " " + os.path.basename(path)

    client = make_client(Model())
    resp = client.post("/infer", data={"path": "bar"})
    assert resp.status_code == 400


def test_path_output_str():
    class Model(cog.Model):
        def setup(self):
            self.foo = "foo"

        @cog.input("text", type=str)
        def run(self, text):
            # TODO(andreas): how to clean up files?
            temp_dir = tempfile.mkdtemp()
            temp_path = os.path.join(temp_dir, "my_file.txt")
            with open(temp_path, "w") as f:
                f.write(self.foo + text)
            return Path(temp_path)

    client = make_client(Model())
    resp = client.post("/infer", data={"text": "baz"})
    assert resp.status_code == 200
    assert resp.content_type == "text/plain; charset=utf-8"
    assert resp.data == b"foobaz"


def test_path_output_image():
    class Model(cog.Model):
        def setup(self):
            pass

        def run(self):
            temp_dir = tempfile.mkdtemp()
            temp_path = os.path.join(temp_dir, "my_file.bmp")
            img = Image.new("RGB", (255, 255), "red")
            img.save(temp_path)
            return Path(temp_path)

    client = make_client(Model())
    resp = client.post("/infer")
    assert resp.status_code == 200
    assert resp.content_type == "image/bmp"
    assert resp.content_length == 195894


def test_multiple_arguments():
    class Model(cog.Model):
        def setup(self):
            self.foo = "foo"

        @cog.input("text", type=str)
        @cog.input("num1", type=int)
        @cog.input("num2", type=int, default=10)
        @cog.input("path", type=Path)
        def run(self, text, num1, num2, path):
            with open(path) as f:
                path_contents = f.read()
            return self.foo + " " + text + " " + str(num1 * num2) + " " + path_contents

    client = make_client(Model())
    path_data = (io.BytesIO(b"bar"), "foo.txt")
    resp = client.post(
        "/infer",
        data={"text": "baz", "num1": 5, "path": path_data},
        content_type="multipart/form-data",
    )
    assert resp.status_code == 200
    assert resp.data == b"foo baz 50 bar"
