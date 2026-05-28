from collections.abc import AsyncIterator

import pytest

from cog import concurrent


def test_concurrent_bare_decorator_sets_default_max() -> None:
    @concurrent
    async def run() -> str:
        return "ok"

    assert run.__cog_concurrent_max__ == 1


def test_concurrent_call_form_sets_default_max() -> None:
    async def run() -> str:
        return "ok"

    decorated = concurrent()(run)

    assert decorated is run
    assert run.__cog_concurrent_max__ == 1


def test_concurrent_max_sets_metadata() -> None:
    async def run() -> str:
        return "ok"

    decorated = concurrent(max=5)(run)

    assert decorated is run
    assert run.__cog_concurrent_max__ == 5


def test_concurrent_rejects_max_less_than_one() -> None:
    with pytest.raises(ValueError, match="max must be at least 1"):
        concurrent(max=0)


@pytest.mark.parametrize("value", [1.5, "2", True])
def test_concurrent_rejects_non_integer_max(value: object) -> None:
    with pytest.raises(TypeError, match="max must be an integer"):
        concurrent(max=value)  # type: ignore[arg-type]


def test_concurrent_rejects_sync_function_with_max_above_one() -> None:
    def run() -> str:
        return "ok"

    with pytest.raises(TypeError, match="max > 1 requires an async"):
        concurrent(max=2)(run)


def test_concurrent_allows_sync_function_with_max_one() -> None:
    def run() -> str:
        return "ok"

    decorated = concurrent(max=1)(run)

    assert decorated is run
    assert run.__cog_concurrent_max__ == 1


def test_concurrent_allows_async_generator_with_max_above_one() -> None:
    async def run() -> AsyncIterator[str]:
        yield "ok"

    decorated = concurrent(max=2)(run)

    assert decorated is run
    assert run.__cog_concurrent_max__ == 2
