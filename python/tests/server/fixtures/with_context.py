from cog import current_scope, Input

class Predictor:
    def predict(self, name: str = Input()):
        prefix = current_scope().context["prefix"]
        return f"{prefix} {name}!"

