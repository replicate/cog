class Predictor:
    def predict(self, text: str) -> str:
        return f"hello {text}"

    def healthcheck(self) -> bool:
        """Raises an exception."""
        raise RuntimeError("Healthcheck failed with error")
