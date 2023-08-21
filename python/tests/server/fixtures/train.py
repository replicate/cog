from cog import BaseModel, Input, Path


class TrainingOutput(BaseModel):
    weights: Path


def train(
    n: int = Input(description="Dimension of weights to generate"),
) -> TrainingOutput:
    with open("weights.bin", "w") as fh:
        for _ in range(n):
            fh.write("a")

    return TrainingOutput(
        weights=Path("weights.bin"),
    )
