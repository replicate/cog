import time


class Predictor:
    def predict(self, text: str) -> str:
        return f"hello {text}"

    def healthcheck(self) -> bool:
        """Times out the healthcheck."""
        time.sleep(10)
        return True
