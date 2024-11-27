from cog import emit_metric


class Predictor:
    def setup(self):
        print("did setup")

    def predict(self, *, name: str) -> str:
        print(f"hello, {name}")

        emit_metric("foo", 123)

        return f"hello, {name}"
