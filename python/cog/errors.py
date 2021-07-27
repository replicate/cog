class CogError(Exception):
    """Base class for all Cog errors."""


class ConfigDoesNotExist(CogError):
    """Exception raised when a cog.yaml does not exist."""


class ModelNotSet(CogError):
    """Exception raised when 'model' is not set in cog.yaml when it needs to be."""
