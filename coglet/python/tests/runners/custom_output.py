from dataclasses import dataclass

from cog import BaseModel, BasePredictor


class CustomOut(BaseModel):
    x: int
    y: str


class ComplexOut(BaseModel):
    a: CustomOut
    b: CustomOut


@dataclass
class CustomDataclassOut:
    x: int
    y: str


@dataclass
class ComplexDataclassOut(BaseModel):
    a: CustomDataclassOut
    b: CustomDataclassOut


class Predictor(BasePredictor):
    test_inputs = {'i': 3}

    def predict(self, i: int) -> list[CustomOut]:
        outputs: list[CustomOut] = []
        while i > 0:
            outputs.append(CustomOut(x=i, y='a'))
            i -= 1
        return outputs


class ComplexOutputPredictor(BasePredictor):
    test_inputs = {'i': 3}

    def predict(self, i: int) -> ComplexOut:
        return ComplexOut(a=CustomOut(x=i, y='a'), b=CustomOut(x=i, y='b'))


class CustomDataclassOutputPredictor(BasePredictor):
    test_inputs = {'i': 3}

    def predict(self, i: int) -> CustomDataclassOut:
        return CustomDataclassOut(x=i, y='a')


class ComplexDataclassOutputPredictor(BasePredictor):
    test_inputs = {'i': 3}

    def predict(self, i: int) -> ComplexDataclassOut:
        return ComplexDataclassOut(
            a=CustomDataclassOut(x=i, y='a'), b=CustomDataclassOut(x=i, y='b')
        )
