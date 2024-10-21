from enum import Enum


class Mode(Enum):
    """Enumeration over the different prediction modes."""

    PREDICT = "predict"
    TRAIN = "train"

    def __str__(self) -> str:
        return str(self.value)
