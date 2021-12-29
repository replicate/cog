from fastapi import FastAPI, encoders

from ..predictor import Predictor, get_predict_types, load_predictor
from ..response import create_response


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
    CogResponse = create_response(OutputType)

    if InputType:

        def predict(input: InputType):
            return CogResponse(status="success", output=predictor.predict(input))

    else:

        def predict():
            return CogResponse(status="success", output=predictor.predict())

    app.post("/predict", response_model=CogResponse)(predict)

    return app


if __name__ == "__main__":
    predictor = load_predictor()
    # TODO
