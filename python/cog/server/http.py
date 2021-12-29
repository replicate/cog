from collections.abc import Generator
import types
import typing
from fastapi import Body, FastAPI, encoders

from ..predictor import Predictor, get_predict_types, load_predictor
from ..response import get_response_type


orig_jsonable_encoder = encoders.jsonable_encoder

# HACK: implement https://github.com/tiangolo/fastapi/pull/2061
def jsonable_encoder(obj, **kwargs):
    custom_encoder = kwargs.get("custom_encoder")
    if custom_encoder:
        if type(obj) in custom_encoder:
            return custom_encoder[type(obj)](obj)
        else:
            for encoder_type, encoder_instance in custom_encoder.items():
                if isinstance(obj, encoder_type):
                    return encoder_instance(obj)
    return orig_jsonable_encoder(obj, **kwargs)


encoders.jsonable_encoder = jsonable_encoder


def create_app(predictor: Predictor) -> FastAPI:
    app = FastAPI()
    app.on_event("startup")(predictor.setup)

    InputType, OutputType = get_predict_types(predictor)

    # We don't support generators for HTTP, so convert it to the type that is yielded
    if typing.get_origin(OutputType) is Generator:
        OutputType = typing.get_args(OutputType)[0]

    CogResponse = get_response_type(OutputType)

    def predict(input: InputType = Body(default=None)):
        if input is None:
            output = predictor.predict()
        else:
            output = predictor.predict(input)

        # loop over generator function to get the last result
        if isinstance(output, types.GeneratorType):
            last_result = None
            for iteration in enumerate(output):
                last_result = iteration
            # last result is a tuple with (index, value)
            output = last_result[1]

        return CogResponse(status="success", output=output)

    app.post("/predict", response_model=CogResponse)(predict)

    return app


if __name__ == "__main__":
    predictor = load_predictor()
    # TODO
