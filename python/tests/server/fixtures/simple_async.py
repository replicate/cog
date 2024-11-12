class Predictor:
    def setup(self):
        print("did setup")

    async def predict(self):
        print("did predict")
        return "prediction output"
