import json
import os
import tempfile

import cog
from cog.json import encode_json
import numpy as np
from pydantic import BaseModel


def test_encode_json_encodes_pydantic_models():
    class Model(BaseModel):
        text: str
        number: int

    assert encode_json(Model(text="hello", number=5), None) == {
        "text": "hello",
        "number": 5,
    }


# TODO
# def test_file():
#     class Model(BaseModel):
#         path: cog.Path

#         class Config:
#             json_encoders = get_json_encoders()

#     temp_dir = tempfile.mkdtemp()
#     temp_path = os.path.join(temp_dir, "my_file.txt")
#     with open(temp_path, "w") as fh:
#         fh.write("file content")
#     model = Model(path=cog.Path(temp_path))
#     assert json.loads(model.json()) == {
#         "path": "data:text/plain;base64,ZmlsZSBjb250ZW50"
#     }


# def test_numpy():
#     class Model(BaseModel):
#         ndarray: np.ndarray
#         npfloat: np.float64
#         npinteger: np.integer

#         class Config:
#             json_encoders = get_json_encoders()
#             arbitrary_types_allowed = True

#     model = Model(
#         ndarray=np.array([[1, 2], [3, 4]]),
#         npfloat=np.float64(1.3),
#         npinteger=np.int32(5),
#     )
#     assert json.loads(model.json()) == {
#         "ndarray": [[1, 2], [3, 4]],
#         "npfloat": 1.3,
#         "npinteger": 5,
#     }
