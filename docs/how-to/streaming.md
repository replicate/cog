# How to stream output

This guide shows you how to stream prediction output from your Cog model so clients receive partial results as they are generated, rather than waiting for the entire prediction to complete.

## Stream text tokens

To stream text output token by token, annotate your `predict()` method with `Iterator[str]` and use `yield` instead of `return`:

```python
from cog import BasePredictor, Input
from typing import Iterator

class Predictor(BasePredictor):
    def setup(self):
        self.model = load_model()

    def predict(self, prompt: str = Input(description="Input prompt")) -> Iterator[str]:
        for token in self.model.generate(prompt):
            yield token
```

Each `yield` sends the value to the client immediately. The HTTP response contains a list of all yielded values.

## Concatenate streamed text

If you want the final output to be a single concatenated string rather than a list of tokens, use `ConcatenateIterator`. This is useful for language models where the consumer wants the full text as a single string:

```python
from cog import BasePredictor, ConcatenateIterator, Input

class Predictor(BasePredictor):
    def predict(self, prompt: str = Input(description="Input prompt")) -> ConcatenateIterator[str]:
        for token in self.model.generate(prompt):
            yield token + " "
```

With `ConcatenateIterator`, platforms like Replicate display the output as a single string instead of an array.

## Stream files

To stream multiple files as they are generated (for example, intermediate images from a diffusion model), use `Iterator[Path]`:

```python
import tempfile
from cog import BasePredictor, Input, Path
from typing import Iterator

class Predictor(BasePredictor):
    def predict(self, prompt: str = Input(description="Input prompt")) -> Iterator[Path]:
        for i, image in enumerate(self.model.generate_steps(prompt)):
            output_path = Path(tempfile.mkdtemp()) / f"step-{i}.png"
            image.save(output_path)
            yield output_path
```

Each yielded `Path` is sent to the client as a separate file output. Cog automatically cleans up temporary files after they have been returned.

## Stream from async predictors

If your predictor uses `async def predict()`, use `AsyncIterator` and `AsyncConcatenateIterator` from the `cog` package instead of `typing.Iterator`:

### Async text streaming

```python
from cog import AsyncIterator, BasePredictor, Input

class Predictor(BasePredictor):
    async def predict(self, prompt: str = Input(description="Input prompt")) -> AsyncIterator[str]:
        async for token in self.model.generate(prompt):
            yield token
```

### Async concatenated text streaming

```python
from cog import AsyncConcatenateIterator, BasePredictor, Input

class Predictor(BasePredictor):
    async def predict(self, prompt: str = Input(description="Input prompt")) -> AsyncConcatenateIterator[str]:
        async for token in self.model.generate(prompt):
            yield token + " "
```

### Async file streaming

```python
import tempfile
from cog import AsyncIterator, BasePredictor, Input, Path

class Predictor(BasePredictor):
    async def predict(self, prompt: str = Input(description="Input prompt")) -> AsyncIterator[Path]:
        async for i, image in self.model.generate_steps(prompt):
            output_path = Path(tempfile.mkdtemp()) / f"step-{i}.png"
            image.save(output_path)
            yield output_path
```

## Receive streaming output via webhooks

When calling the HTTP API asynchronously, streaming output is delivered through webhooks. Each time `predict()` yields a value, the server sends a webhook with the `output` event type.

```console
curl http://localhost:5001/predictions -X POST \
    -H "Content-Type: application/json" \
    -H "Prefer: respond-async" \
    -d '{
        "input": {"prompt": "hello"},
        "webhook": "https://example.com/webhook",
        "webhook_events_filter": ["output", "completed"]
    }'
```

Webhook requests for `output` events are batched and sent at most once every 500ms. See the [HTTP API reference](../http.md#webhooks) for full details.

## Supported types for streaming

You can stream any of these types with `Iterator` or `AsyncIterator`:

- `str`
- `int`
- `float`
- `bool`
- `cog.Path`

`ConcatenateIterator` and `AsyncConcatenateIterator` are only meaningful with `str`.

## Next steps

- See the [prediction interface reference](../python.md#streaming-output) for the full streaming API.
- See [How to run concurrent predictions](concurrency.md) to combine streaming with async concurrency.
- See the [HTTP API reference](../http.md) for details on how streaming output is delivered over HTTP.
