# pytest cog_test.py

import time
import shutil
import tempfile
import io
import os
from pathlib import Path

import pytest
from flask.testing import FlaskClient
from PIL import Image

import cog


def make_client(model) -> FlaskClient:
    app = cog.HTTPServer(model).make_app()
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
            num2 = num ** 3
            return self.foo + str(num2)

    client = make_client(Model())
    resp = client.post("/infer", data={"num": 3})
    assert resp.status_code == 200
    assert resp.data == b"foo27"
    resp = client.post("/infer", data={"num": -3})
    assert resp.status_code == 200
    assert resp.data == b"foo-27"


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


def test_good_float_input():
    class Model(cog.Model):
        def setup(self):
            self.foo = "foo"

        @cog.input("num", type=float)
        def run(self, num):
            num2 = num ** 3
            return self.foo + str(num2)

    client = make_client(Model())
    resp = client.post("/infer", data={"num": 3})
    assert resp.status_code == 200
    assert resp.data == b"foo27.0"
    resp = client.post("/infer", data={"num": 3.5})
    assert resp.status_code == 200
    assert resp.data == b"foo42.875"
    resp = client.post("/infer", data={"num": -3.5})
    assert resp.status_code == 200
    assert resp.data == b"foo-42.875"


def test_bad_float_input():
    class Model(cog.Model):
        def setup(self):
            self.foo = "foo"

        @cog.input("num", type=float)
        def run(self, num):
            num2 = num ** 2
            return self.foo + str(num2)

    client = make_client(Model())
    resp = client.post("/infer", data={"num": "foo"})
    assert resp.status_code == 400


def test_good_bool_input():
    class Model(cog.Model):
        def setup(self):
            self.foo = "foo"

        @cog.input("flag", type=bool)
        def run(self, flag):
            if flag:
                return self.foo + "yes"
            else:
                return self.foo + "no"

    client = make_client(Model())
    resp = client.post("/infer", data={"flag": True})
    assert resp.status_code == 200
    assert resp.data == b"fooyes"
    resp = client.post("/infer", data={"flag": False})
    assert resp.status_code == 200
    assert resp.data == b"foono"


def test_bad_float_input():
    class Model(cog.Model):
        def setup(self):
            self.foo = "foo"

        @cog.input("flag", type=bool)
        def run(self, flag):
            if flag:
                return self.foo + "yes"
            else:
                return self.foo + "no"

    client = make_client(Model())
    resp = client.post("/infer", data={"flag": "foo"})
    assert resp.status_code == 400


def test_min_max():
    class Model(cog.Model):
        def setup(self):
            pass

        @cog.input("num1", type=float, min=3, max=10.5)
        @cog.input("num2", type=float, min=-4)
        @cog.input("num3", type=int, max=-4)
        def run(self, num1, num2, num3):
            return num1 + num2 + num3

    client = make_client(Model())
    resp = client.post("/infer", data={"num1": 3, "num2": -4, "num3": -4})
    assert resp.status_code == 200
    assert resp.data == b"-5.0\n"
    resp = client.post("/infer", data={"num1": 2, "num2": -4, "num3": -4})
    assert resp.status_code == 400
    resp = client.post("/infer", data={"num1": 3, "num2": -4.1, "num3": -4})
    assert resp.status_code == 400
    resp = client.post("/infer", data={"num1": 3, "num2": -4, "num3": -3})
    assert resp.status_code == 400


def test_good_options():
    class Model(cog.Model):
        def setup(self):
            pass

        @cog.input("text", type=str, options=["foo", "bar"])
        @cog.input("num", type=int, options=[1, 2, 3])
        def run(self, text, num):
            return text + ("a" * num)

    client = make_client(Model())
    resp = client.post("/infer", data={"text": "foo", "num": 2})
    assert resp.status_code == 200
    assert resp.data == b"fooaa"


def test_bad_options_type():
    with pytest.raises(ValueError):

        class Model(cog.Model):
            def setup(self):
                pass

            @cog.input("text", type=str, options=[])
            def run(self, text):
                return text

    with pytest.raises(ValueError):

        class Model(cog.Model):
            def setup(self):
                pass

            @cog.input("text", type=str, options=["foo"])
            def run(self, text):
                return text

    with pytest.raises(ValueError):

        class Model(cog.Model):
            def setup(self):
                pass

            @cog.input("text", type=Path, options=["foo"])
            def run(self, text):
                return text


def test_bad_options():
    class Model(cog.Model):
        def setup(self):
            pass

        @cog.input("text", type=str, options=["foo", "bar"])
        @cog.input("num", type=int, options=[1, 2, 3])
        def run(self, text, num):
            return text + ("a" * num)

    client = make_client(Model())
    resp = client.post("/infer", data={"text": "baz", "num": 2})
    assert resp.status_code == 400
    resp = client.post("/infer", data={"text": "bar", "num": 4})
    assert resp.status_code == 400


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


def test_default_path_input():
    class Model(cog.Model):
        def setup(self):
            self.foo = "foo"

        @cog.input("path", type=Path, default=None)
        def run(self, path):
            if path is None:
                return "noneee"
            with open(path) as f:
                return self.foo + " " + f.read() + " " + os.path.basename(path)

    client = make_client(Model())
    path_data = (io.BytesIO(b"bar"), "foo.txt")
    resp = client.post(
        "/infer", data={"path": path_data}, content_type="multipart/form-data"
    )
    assert resp.status_code == 200
    assert resp.data == b"foo bar foo.txt"
    resp = client.post("/infer", data={})
    assert resp.status_code == 200
    assert resp.data == b"noneee"


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


def test_help():
    class Model(cog.Model):
        def setup(self):
            self.foo = "foo"

        @cog.input("text", type=str, help="Some text")
        @cog.input("num1", type=int, help="First number")
        @cog.input("num2", type=int, default=10, help="Second number")
        @cog.input("path", type=Path, help="A file path")
        def run(self, text, num1, num2, path):
            with open(path) as f:
                path_contents = f.read()
            return self.foo + " " + text + " " + str(num1 * num2) + " " + path_contents

    client = make_client(Model())
    resp = client.get("/help")
    assert resp.status_code == 200
    assert resp.json == {
        "arguments": {
            "text": {
                "type": "str",
                "help": "Some text",
            },
            "num1": {
                "type": "int",
                "help": "First number",
            },
            "num2": {
                "type": "int",
                "help": "Second number",
                "default": "10",
            },
            "path": {
                "type": "Path",
                "help": "A file path",
            },
        }
    }


def test_timing():
    class ModelSlow(cog.Model):
        def setup(self):
            time.sleep(0.5)

        def run(self):
            time.sleep(0.5)
            return ""

    class ModelFast(cog.Model):
        def setup(self):
            pass

        def run(self):
            return ""

    client = make_client(ModelSlow())
    resp = client.post("/infer")
    assert resp.status_code == 200
    assert 0.5 < float(resp.headers["X-Setup-Time"]) < 1.0
    assert 0.5 < float(resp.headers["X-Run-Time"]) < 1.0

    client = make_client(ModelFast())
    resp = client.post("/infer")
    assert resp.status_code == 200
    assert float(resp.headers["X-Setup-Time"]) < 0.5
    assert float(resp.headers["X-Run-Time"]) < 0.5


def test_unzip_to_tempdir(tmpdir_factory):
    input_dir = tmpdir_factory.mktemp("input")
    with open(os.path.join(input_dir, "hello.txt"), "w") as f:
        f.write("hello")
    os.mkdir(os.path.join(input_dir, "mydir"))
    with open(os.path.join(input_dir, "mydir", "world.txt"), "w") as f:
        f.write("world")

    zip_dir = tmpdir_factory.mktemp("zip")
    zip_path = os.path.join(zip_dir, "my-archive.zip")

    shutil.make_archive(zip_path.split(".zip")[0], "zip", input_dir)
    with cog.unzip_to_tempdir(zip_path) as tempdir:
        with open(os.path.join(tempdir, "hello.txt")) as f:
            assert f.read() == "hello"
        with open(os.path.join(tempdir, "mydir", "world.txt")) as f:
            assert f.read() == "world"
