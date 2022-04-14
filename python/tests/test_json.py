import json
import os
import tempfile

import cog
from cog.files import upload_file
from cog.json import encode_json
import numpy as np
from pydantic import BaseModel


def test_encode_json_recursively_encodes_tuples():
    result = encode_json((np.float32(0.1), np.float32(0.2)), None)
    assert type(result[0]) == float


def test_encode_json_encodes_pydantic_models():
    class Model(BaseModel):
        text: str
        number: int

    assert encode_json(Model(text="hello", number=5), None) == {
        "text": "hello",
        "number": 5,
    }


def test_encode_json_uploads_files():
    class Model(BaseModel):
        path: cog.Path

    temp_dir = tempfile.mkdtemp()
    temp_path = os.path.join(temp_dir, "my_file.txt")
    with open(temp_path, "w") as fh:
        fh.write("file content")
    model = Model(path=cog.Path(temp_path))
    assert encode_json(model, upload_file) == {
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
    assert encode_json(model, None) == {
        "ndarray": [[1, 2], [3, 4]],
        "npfloat": 1.3,
        "npinteger": 5,
    }
