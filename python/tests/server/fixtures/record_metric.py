from cog import current_scope


class Predictor:
    def setup(self):
        print("did setup")

    def predict(self, *, name: str) -> str:
        print(f"hello, {name}")

        current_scope().record_metric("foo", 123)

        return f"hello, {name}"
