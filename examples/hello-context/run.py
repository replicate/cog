import warnings

from cog import current_scope, Input, ExperimentalFeatureWarning

warnings.filterwarnings(action="ignore", category=ExperimentalFeatureWarning)


def run(
    text: str = Input(description="Example text input"),
) -> dict[str, dict[str, str]]:
    return {"inputs": {"text": text}, "context": current_scope().context}
