from cog.errors import CogError, ConfigDoesNotExist, PredictorNotSet


def test_error_types_remain_importable() -> None:
    assert issubclass(ConfigDoesNotExist, CogError)
    assert issubclass(PredictorNotSet, CogError)
