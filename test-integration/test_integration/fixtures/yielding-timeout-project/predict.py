import cog
import time


class Predictor(cog.Predictor):
    def setup(self):
        pass

    @cog.input("sleep_time", type=float)
    @cog.input("n_iterations", type=int)
    def predict(self, sleep_time, n_iterations):
        for i in range(n_iterations):
            time.sleep(sleep_time)
            yield f"yield {i}"
