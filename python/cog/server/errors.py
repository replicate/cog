class CogServerError(Exception):
    """Base class for all Cog server errors."""

    def __str__(self) -> str:
        return f"Cog: {super().__str__()}"


class FileUploadError(CogServerError):
    pass


class RunnerBusyError(CogServerError):
    pass


class UnknownPredictionError(CogServerError):
    pass


class CogRuntimeError(CogServerError, RuntimeError):
    pass


class CogTimeoutError(CogServerError, TimeoutError):
    pass
