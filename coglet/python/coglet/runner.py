import importlib
import inspect
import os
import os.path
from typing import Any, AsyncGenerator, Callable, Dict, Optional

from coglet import adt, api, inspector


class FunctionPredictor(api.BasePredictor):
    setup_done = False

    def __init__(self, _predict: Callable, test_inputs: Optional[Any]):
        self._predict = _predict
        self.test_inputs = test_inputs

    def setup(self, weights=None) -> None:
        self.setup_done = True

    def predict(self, **kwargs: Any) -> Any:
        return self._predict(**kwargs)


class AsyncFunctionPredictor(api.BasePredictor):
    setup_done = False

    def __init__(self, _predict: Callable, test_inputs: Optional[Any]):
        self._predict = _predict
        self.test_inputs = test_inputs

    def setup(self, weights=None) -> None:
        self.setup_done = True

    async def predict(self, **kwargs: Any) -> Any:
        return await self._predict(**kwargs)


class AsyncIteratorFunctionPredictor(api.BasePredictor):
    setup_done = False

    def __init__(self, _predict: Callable, test_inputs: Optional[Any]):
        self._predict = _predict
        self.test_inputs = test_inputs

    def setup(self, weights=None) -> None:
        self.setup_done = True

    async def predict(self, **kwargs: Any) -> Any:
        async for item in self._predict(**kwargs):
            yield item


def create_function_predictor(
    p: Callable, *, test_inputs: Optional[Any] = None
) -> api.BasePredictor:
    if inspect.iscoroutinefunction(p):
        return AsyncFunctionPredictor(p, test_inputs)

    elif inspect.isasyncgenfunction(p):
        return AsyncIteratorFunctionPredictor(p, test_inputs)

    return FunctionPredictor(p, test_inputs)


# Reflectively run a Cog predictor
# async by default and just run non-async setup/predict by blocking the caller
class Runner:
    predictor: api.BasePredictor

    def __init__(self, predictor: adt.Predictor):
        self.inputs = predictor.inputs
        self.output = predictor.output

        module = importlib.import_module(predictor.module_name)
        p = getattr(module, predictor.predictor_name)
        if inspect.isclass(p) and issubclass(p, api.BasePredictor):
            self.predictor = p()
        elif inspect.isfunction(p):
            self.predictor = create_function_predictor(
                p, test_inputs=getattr(module, 'test_inputs', None)
            )
        else:
            raise RuntimeError(
                f'invalid predictor {predictor.module_name}.{predictor.predictor_name}'
            )
        self.is_async_predict = inspect.iscoroutinefunction(
            self.predictor.predict
        ) or inspect.isasyncgenfunction(self.predictor.predict)

    async def test(self) -> Any:
        inputs = inspector.get_test_inputs(self.predictor, self.inputs)
        for k, v in inputs.items():
            tpe = self.inputs[k].type
            w = tpe.json_decode(tpe.json_encode(v))
            assert w == v, f'test input {k} does not encode properly'
        await self.setup()
        if self.is_iter():
            output = []
            async for x in self.predict_iter(inputs):
                self.output.json_encode(x)
                output.append(x)
        else:
            output = await self.predict(inputs)
            self.output.json_encode(output)
        return output

    async def setup(self) -> None:
        kwargs: Dict[str, Any] = {}
        if 'weights' in inspect.signature(self.predictor.setup).parameters:
            url = os.environ.get('COG_WEIGHTS')
            path = 'weights'
            if url:
                kwargs['weights'] = url
                self.predictor.setup(weights=url)
            elif os.path.exists(path):
                kwargs['weights'] = api.Path(path)
                self.predictor.setup(weights=api.Path(path))
            else:
                kwargs['weights'] = None
        if inspect.iscoroutinefunction(self.predictor.setup):
            return await self.predictor.setup(**kwargs)
        else:
            return self.predictor.setup(**kwargs)

    # functions can return regular values or generators, not both
    def is_iter(self) -> bool:
        return self.output.kind in {
            adt.Kind.ITERATOR,
            adt.Kind.CONCAT_ITERATOR,
        }

    async def predict(self, inputs: Dict[str, Any]) -> Any:
        assert not self.is_iter(), 'predict returns iterator, call predict_iter instead'
        kwargs = inspector.check_input(self.inputs, inputs)
        if self.is_async_predict:
            output = await self.predictor.predict(**kwargs)
        else:
            output = self.predictor.predict(**kwargs)
        return self.output.normalize(output)

    async def predict_iter(self, inputs: Dict[str, Any]) -> AsyncGenerator[Any, None]:
        assert self.is_iter(), 'predict does not return iterator, call predict instead'
        assert self.output.type is not None, 'missing output type'

        kwargs = inspector.check_input(self.inputs, inputs)
        if self.is_async_predict:
            async for x in self.predictor.predict(**kwargs):
                yield self.output.normalize(x)
        else:
            for x in self.predictor.predict(**kwargs):
                yield self.output.normalize(x)
