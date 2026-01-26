class Predictor:
    def predict(self, text: str) -> str:
        return f"hello {text}"

    def healthcheck(self) -> bool:
        """Always returns unhealthy."""
        return False
