class CogError(Exception):
    """Base class for all Cog errors."""


class ConfigDoesNotExist(CogError):
    """Exception raised when a cog.yaml does not exist."""


class PredictorNotSet(CogError):
    """Exception raised when 'predict' is not set in cog.yaml when it needs to be."""


class IrrecoverablePredictorFailure(CogError):
    """Exception raised by a predictor when it cannot run any more predictions."""
