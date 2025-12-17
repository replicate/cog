from cog import BasePredictor, Input, Path


class Predictor(BasePredictor):
    """Test predictor that exactly matches the user's scenario with Path = Input(...)"""

    def predict(
        self,
        img: Path = Input(
            description='Reference image of the character whose face to swap'
        ),
    ) -> Path:
        print('img', type(img), img)
        return img

    @property
    def test_inputs(self):
        return {
            'img': 'https://httpbin.org/image/jpeg',
        }
