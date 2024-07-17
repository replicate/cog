from cog import BaseModel, Input, Path

class TrainingOutput(BaseModel):
    weights: Path

def train(
    n: int,
) -> TrainingOutput:
    with open("weights.bin", "w") as fh:
        for _ in range(n):
            fh.write("a")

    return TrainingOutput(
        weights=Path("weights.bin"),
    )
