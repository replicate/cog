from collections.abc import Set
from dataclasses import dataclass, field
from enum import Enum


class RuntimeMetric(str, Enum):
    """Stable Cog runtime metrics that a model may disable."""

    PREDICTION_COUNT = "prediction_count"
    PREDICTION_REJECTED = "prediction_rejected"
    PREDICTION_ACTIVE = "prediction_active"
    PREDICTION_DURATION = "prediction_duration"
    SETUP_DURATION = "setup_duration"
    SLOT_COUNT = "slot_count"


@dataclass(frozen=True)
class RuntimeMetricsConfig:
    """Configure Cog's parent-owned runtime metrics."""

    enabled: bool = True
    disabled: Set[RuntimeMetric] = field(default_factory=frozenset)

    def __post_init__(self) -> None:
        _validate_enabled(self.enabled)
        object.__setattr__(self, "disabled", _validate_disabled(self.disabled))


def _validate_enabled(value: object) -> None:
    if not isinstance(value, bool):
        raise TypeError("RuntimeMetricsConfig.enabled must be a bool")


def _validate_disabled(value: object) -> frozenset[RuntimeMetric]:
    if not isinstance(value, Set):
        raise TypeError(
            "RuntimeMetricsConfig.disabled must be a set-like collection of RuntimeMetric values"
        )
    invalid = [metric for metric in value if not isinstance(metric, RuntimeMetric)]
    if invalid:
        valid = ", ".join(metric.value for metric in RuntimeMetric)
        raise ValueError(
            "RuntimeMetricsConfig.disabled must contain RuntimeMetric values "
            f"({valid}); got {invalid!r}"
        )
    return frozenset(value)
