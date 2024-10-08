class CogError(Exception):
    """Base class for all Cog errors."""


class ConfigDoesNotExist(CogError):
    """Exception raised when a cog.yaml does not exist."""


class PredictorNotSet(CogError):
    """Exception raised when 'predict' is not set in cog.yaml when it needs to be."""


"""Prefix for all Cog errors attributable to internals or infrastructure."""
COG_INTERNAL_ERROR_PREFIX = "cog-internal: "
