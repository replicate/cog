import functools
import inspect
from typing import Any, Callable

from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def general(
        self, prompt: str = Input(description="hi"), system_prompt: str = None
    ) -> int:
        return 1

    def _remove(f: Callable, defaults: "dict[str, Any]") -> Callable:
        # pylint: disable=no-self-argument
        def wrapper(self, *args, **kwargs):
            kwargs.update(defaults)
            return f(self, *args, **kwargs)

        # Update wrapper attributes for documentation, etc.
        functools.update_wrapper(wrapper, f)

        # for the purposes of inspect.signature as used by predictor.get_input_type,
        # remove the argument (system_prompt)
        sig = inspect.signature(f)
        params = [p for name, p in sig.parameters.items() if name not in defaults]
        wrapper.__signature__ = sig.replace(parameters=params)

        # Return partialmethod, wrapper behaves correctly when part of a class
        return functools.partialmethod(wrapper)

    predict = _remove(general, {"system_prompt": ""})


def _train(prompt: str = Input(description="hi"), system_prompt: str = None) -> int:
    return 1


train = functools.partial(_train, system_prompt="")
