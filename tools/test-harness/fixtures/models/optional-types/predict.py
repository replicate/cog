from typing import Optional

from cog import BasePredictor, File, Input, Path


class Predictor(BasePredictor):
    def predict(
        self,
        text: str = Input(description="Required string"),
        # PEP 604 style optionals
        opt_str: str | None = Input(description="Optional string", default=None),
        opt_int: int | None = Input(description="Optional int", default=None),
        opt_float: float | None = Input(description="Optional float", default=None),
        opt_bool: bool | None = Input(description="Optional bool", default=None),
        # typing.Optional style
        opt_path: Optional[Path] = Input(description="Optional path", default=None),
        opt_file: Optional[File] = Input(description="Optional file", default=None),
    ) -> str:
        return f"{text}"
