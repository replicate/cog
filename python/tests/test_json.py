from asyncore import file_dispatcher
import json
import os
import tempfile

from black import io
import cog
from cog.json import JSON_ENCODERS
import numpy as np
from pydantic import BaseModel


def test_file():
    class Model(BaseModel):
        path: cog.Path

        class Config:
            json_encoders = JSON_ENCODERS

    temp_dir = tempfile.mkdtemp()
    temp_path = os.path.join(temp_dir, "my_file.txt")
    with open(temp_path, "w") as fh:
        fh.write("file content")
    model = Model(path=cog.Path(temp_path))
    assert json.loads(model.json()) == {
        "path": "data:text/plain;base64,ZmlsZSBjb250ZW50"
    }


def test_numpy():
    class Model(BaseModel):
        ndarray: np.ndarray
        npfloat: np.float
        npinteger: np.integer

        class Config:
            json_encoders = JSON_ENCODERS
            arbitrary_types_allowed = True

    model = Model(
        ndarray=np.array([[1, 2], [3, 4]]),
        npfloat=np.float64(1.3),
        npinteger=np.int32(5),
    )
    assert json.loads(model.json()) == {
        "ndarray": [[1, 2], [3, 4]],
        "npfloat": 1.3,
        "npinteger": 5,
    }
