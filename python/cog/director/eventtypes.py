from attrs import define

from .. import schema


@define
class Webhook:
    payload: schema.PredictionResponse
