from cog import BaseModel, File
import io


class TrainingOutput(BaseModel):
    weights: File


def train(text: str) -> TrainingOutput:
    weights = io.StringIO(text)
    return TrainingOutput(weights=weights)
