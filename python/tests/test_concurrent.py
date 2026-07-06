"""Tests for the cog.concurrent decorator."""

import sys
import types
from collections.abc import AsyncIterator

import pytest

if "coglet" not in sys.modules:
    coglet = types.ModuleType("coglet")
    coglet.CancelationException = type("CancelationException", (BaseException,), {})
    sys.modules["coglet"] = coglet

from cog import concurrent


def test_bare_concurrent_sets_default_max_on_sync_function() -> None:
    @concurrent
    def predict() -> str:
        return "ok"

    assert predict.__cog_concurrent_max__ == 1


def test_concurrent_call_form_sets_default_max_on_sync_function() -> None:
    @concurrent()
    def predict() -> str:
        return "ok"

    assert predict.__cog_concurrent_max__ == 1


def test_concurrent_max_one_allows_sync_function() -> None:
    @concurrent(max=1)
    def predict() -> str:
        return "ok"

    assert predict.__cog_concurrent_max__ == 1


def test_concurrent_max_allows_async_function() -> None:
    @concurrent(max=3)
    async def predict() -> str:
        return "ok"

    assert predict.__cog_concurrent_max__ == 3


def test_concurrent_max_allows_async_generator() -> None:
    @concurrent(max=3)
    async def predict() -> AsyncIterator[str]:
        yield "ok"

    assert predict.__cog_concurrent_max__ == 3


def test_concurrent_max_rejects_sync_function() -> None:
    with pytest.raises(TypeError, match="requires an async function"):

        @concurrent(max=2)
        def predict() -> str:
            return "ok"


@pytest.mark.parametrize("value", [0, -1])
def test_concurrent_rejects_max_less_than_one(value: int) -> None:
    with pytest.raises(ValueError, match="at least 1"):
        concurrent(max=value)


@pytest.mark.parametrize("value", [True, 1.5, "2"])
def test_concurrent_rejects_non_integer_max(value: object) -> None:
    with pytest.raises(TypeError, match="must be an integer"):
        concurrent(max=value)  # type: ignore[arg-type]


def test_concurrent_rejects_positional_argument() -> None:
    with pytest.raises(TypeError, match="@concurrent or @concurrent\\(max=...\\)"):
        concurrent(2)  # type: ignore[arg-type]
