class Predictor:
    def setup(self):
        print("did setup")

    def predict(self):
        while True:
            try:
                time.sleep(10)
            except:
                pass
