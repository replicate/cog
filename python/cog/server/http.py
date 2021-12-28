import inspect
import json
from fastapi import FastAPI, Response
from typing import Literal

from pydantic import BaseModel, Field

import cog
from ..predictor import Predictor, get_predict_types, load_predictor


class JSONEncoder(json.JSONEncoder):
    def default(self, obj):
        if isinstance(obj, cog.File):
            return cog.File.encode(obj)
        elif isinstance(obj, cog.Path):
            return cog.Path.encode(obj)
        return json.JSONEncoder.default(self, obj)


def create_app(predictor: Predictor) -> FastAPI:
    app = FastAPI()
    app.on_event("startup")(predictor.setup)

    InputType, OutputType = get_predict_types(predictor)

    class ResponseData(BaseModel):
        status: str = Field(...)
        output: OutputType = Field(...)

        class Config:
            # json_dumps = lambda *args, **kwargs: json.dumps(
            #     *args, cls=JSONEncoder, **kwargs
            # )
            # json_dumps = lambda _: "foo"
            json_encoders = {cog.Path: lambda _: "foo"}

        # json_dumps = lambda _: "foo"

    def create_response(output) -> ResponseData:
        res = ResponseData(status="success", output=output)

        # HACK: https://github.com/tiangolo/fastapi/pull/2061
        return Response(
            content=json.dumps(res.dict(), cls=JSONEncoder),
            media_type="application/json",
        )

    if InputType:

        def predict(input: InputType):
            return create_response(predictor.predict(input))

    else:

        def predict():
            return create_response(predictor.predict())

    app.post("/predict", response_model=ResponseData)(predict)

    return app


if __name__ == "__main__":
    predictor = load_predictor()
    # TODO
