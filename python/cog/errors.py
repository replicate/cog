class CogError(Exception):
    """Base class for all Cog errors."""


class ConfigDoesNotExist(CogError):
    """Exception raised when a cog.yaml does not exist."""
