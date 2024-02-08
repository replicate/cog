import os
import tempfile

import cog
import numpy as np
from cog.files import upload_file
from cog.json import make_encodeable, upload_files
from pydantic import BaseModel


def test_make_encodeable_recursively_encodes_tuples():
    result = make_encodeable((np.float32(0.1), np.float32(0.2)))
    assert isinstance(result[0], float)


def test_make_encodeable_encodes_pydantic_models():
    class Model(BaseModel):
        text: str
        number: int

    assert make_encodeable(Model(text="hello", number=5)) == {
        "text": "hello",
        "number": 5,
    }


def test_make_encodeable_ignores_files():
    class Model(BaseModel):
        path: cog.Path

    temp_dir = tempfile.mkdtemp()
    temp_path = os.path.join(temp_dir, "my_file.txt")
    with open(temp_path, "w") as fh:
        fh.write("file content")
    path = cog.Path(temp_path)
    model = Model(path=path)
    assert make_encodeable(model) == {"path": path}


def test_upload_files():
    temp_dir = tempfile.mkdtemp()
    temp_path = os.path.join(temp_dir, "my_file.txt")
    with open(temp_path, "w") as fh:
        fh.write("file content")
    obj = {"path": cog.Path(temp_path)}
    assert upload_files(obj, upload_file) == {
        "path": "data:text/plain;base64,ZmlsZSBjb250ZW50"
    }


def test_numpy():
    class Model(BaseModel):
        ndarray: np.ndarray
        npfloat: np.float64
        npinteger: np.integer

        class Config:
            arbitrary_types_allowed = True

    model = Model(
        ndarray=np.array([[1, 2], [3, 4]]),
        npfloat=np.float64(1.3),
        npinteger=np.int32(5),
    )
    assert make_encodeable(model) == {
        "ndarray": [[1, 2], [3, 4]],
        "npfloat": 1.3,
        "npinteger": 5,
    }
