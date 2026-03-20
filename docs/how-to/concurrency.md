# How to run concurrent predictions

This guide shows you how to configure your Cog model to process multiple predictions at the same time, reducing latency when your model can handle parallel requests.

Concurrent predictions require Cog 0.14.0 or later.

## Set the concurrency limit

Add `concurrency.max` to your `cog.yaml` to specify how many predictions can run simultaneously:

```yaml
build:
  gpu: true
  python_version: "3.12"
  python_requirements: requirements.txt
concurrency:
  max: 4
predict: "predict.py:Predictor"
```

The value should match what your model can handle given available GPU memory and compute. Start low and increase based on testing.

## Make predict() async

When `concurrency.max` is set, your `predict()` method must be `async`. Change `def predict` to `async def predict`:

```python
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def setup(self):
        self.model = load_model()

    async def predict(self, prompt: str = Input(description="Input prompt")) -> str:
        result = await self.model.generate(prompt)
        return result
```

If your model's inference code is synchronous (which is common for most ML frameworks), use `asyncio.to_thread` to run it without blocking other predictions:

```python
import asyncio
from cog import BasePredictor, Input, Path

class Predictor(BasePredictor):
    def setup(self):
        self.model = load_model()

    async def predict(self, prompt: str = Input(description="Input prompt")) -> str:
        result = await asyncio.to_thread(self.model.generate, prompt)
        return result
```

`asyncio.to_thread` runs the synchronous function in a thread pool, allowing other async predictions to proceed while one is blocked on inference.

## Make setup() async (optional)

If your setup involves async operations (downloading weights from multiple sources in parallel, for example), you can also make `setup()` async:

```python
import asyncio
from cog import BasePredictor

class Predictor(BasePredictor):
    async def setup(self):
        self.tokenizer, self.model = await asyncio.gather(
            download_tokenizer(),
            download_model(),
        )
```

Async `setup()` is optional. You only need it if your setup code benefits from concurrency.

## Handle 409 Conflict responses

When all prediction slots are in use, the server returns `409 Conflict`. Your client should handle this by retrying after a delay:

```python
import time
import requests

def predict_with_retry(url, data, max_retries=5):
    for attempt in range(max_retries):
        response = requests.post(url, json=data)
        if response.status_code == 409:
            time.sleep(0.5 * (attempt + 1))
            continue
        response.raise_for_status()
        return response.json()
    raise RuntimeError("All prediction slots busy after retries")
```

If you use the idempotent `PUT /predictions/<id>` endpoint, retries are safe by design -- the server will not create duplicate predictions for the same ID.

## Combine concurrency with streaming

Async predictors can stream output using `AsyncIterator` or `AsyncConcatenateIterator`:

```python
from cog import AsyncConcatenateIterator, BasePredictor, Input

class Predictor(BasePredictor):
    async def predict(self, prompt: str = Input(description="Input prompt")) -> AsyncConcatenateIterator[str]:
        async for token in self.model.generate_stream(prompt):
            yield token
```

Each concurrent prediction streams independently. See [How to stream output](streaming.md) for more detail.

## Choose the right concurrency limit

- For GPU models, the limit depends on how much VRAM each prediction uses. If each prediction uses 4 GB and you have 24 GB available, `max: 5` is a reasonable starting point.
- For CPU-bound models, the limit depends on the number of cores and how much parallelism your framework supports.
- For models that primarily wait on external API calls, the limit can be much higher (tens or hundreds) since the bottleneck is network I/O, not compute.

Monitor GPU memory usage and prediction latency to find the right value. A concurrency limit that is too high will cause out-of-memory errors or degraded performance.

## Next steps

- See the [`cog.yaml` reference](../yaml.md#concurrency) for concurrency configuration.
- See the [prediction interface reference](../python.md#async-predictors-and-concurrency) for async predictor details.
- See the [HTTP API reference](../http.md) for endpoint behaviour under concurrency.
