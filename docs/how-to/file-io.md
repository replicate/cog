# How to handle file inputs and outputs

This guide shows you how to accept files as input to your model, return files as output, and configure where output files are uploaded.

## Accept a file as input

Use `cog.Path` as the type annotation for any input that should be a file. When the model is called via the HTTP API, clients pass a URL and Cog downloads the file automatically:

```python
from cog import BasePredictor, Input, Path

class Predictor(BasePredictor):
    def predict(self, image: Path = Input(description="Image to process")) -> str:
        # image is a Path pointing to the downloaded file on disk
        with open(image, "rb") as f:
            data = f.read()
        return f"Received {len(data)} bytes"
```

`cog.Path` is a subclass of Python's `pathlib.Path`, so you can use all standard `pathlib` methods on it -- `open()`, `read_bytes()`, `suffix`, etc.

When testing locally with the CLI, prefix file inputs with `@`:

```console
cog predict -i image=@photo.jpg
```

## Accept multiple file inputs

To accept a list of files, use `list[Path]`:

```python
from cog import BasePredictor, Input, Path

class Predictor(BasePredictor):
    def predict(self, images: list[Path] = Input(description="Images to process")) -> str:
        return f"Received {len(images)} images"
```

On the CLI, repeat the input name for each file:

```console
cog predict -i images=@photo1.jpg -i images=@photo2.jpg
```

## Return a single file

To return a file from `predict()`, create a temporary file, write to it, and return a `cog.Path` pointing to it. Cog automatically cleans up temporary files after they have been sent to the client:

```python
import tempfile
from cog import BasePredictor, Input, Path

class Predictor(BasePredictor):
    def predict(self, prompt: str = Input(description="Input prompt")) -> Path:
        image = self.model.generate(prompt)

        output_path = Path(tempfile.mkdtemp()) / "output.png"
        image.save(output_path)
        return output_path
```

The file extension on the output path determines the content type in the response.

## Return multiple files

To return multiple files, annotate the return type as `list[Path]`:

```python
import tempfile
from cog import BasePredictor, Input, Path

class Predictor(BasePredictor):
    def predict(self, prompt: str = Input(description="Input prompt"), count: int = Input(default=3)) -> list[Path]:
        output = []
        for i in range(count):
            image = self.model.generate(prompt, seed=i)
            out_path = Path(tempfile.mkdtemp()) / f"output-{i}.png"
            image.save(out_path)
            output.append(out_path)
        return output
```

Files are named in the format `output.<index>.<extension>` in the response (e.g. `output.0.png`, `output.1.png`).

## Return files alongside other data

To return files alongside scalar values, define an `Output` object:

```python
import tempfile
from cog import BaseModel, BasePredictor, Input, Path
from typing import Optional

class Output(BaseModel):
    image: Path
    caption: str
    confidence: float

class Predictor(BasePredictor):
    def predict(self, prompt: str = Input(description="Input prompt")) -> Output:
        image = self.model.generate(prompt)
        out_path = Path(tempfile.mkdtemp()) / "output.png"
        image.save(out_path)
        return Output(image=out_path, caption="Generated image", confidence=0.95)
```

## Configure file uploads

By default, file outputs are returned as base64-encoded data URLs in the JSON response. For large files, this is impractical.

### Per-request upload URL (synchronous predictions)

Set `output_file_prefix` in the request body to upload files to a remote URL instead:

```console
curl http://localhost:5001/predictions -X POST \
    -H "Content-Type: application/json" \
    -d '{
        "input": {"prompt": "a cat"},
        "output_file_prefix": "https://example.com/upload"
    }'
```

Cog sends a `PUT` request with the file to the specified URL. The response `output` field contains the uploaded file's URL instead of a data URL.

### Server-wide upload URL (asynchronous predictions)

For asynchronous predictions (using the `Prefer: respond-async` header), you must configure the upload URL when starting the server:

```console
cog serve --upload-url https://example.com/upload
```

Or when running the Docker container directly:

```console
docker run -d -p 5001:5000 my-model --upload-url https://example.com/upload
```

See the [HTTP API reference on file uploads](../http.md#file-uploads) for the full upload protocol.

## A note on `cog.File`

`cog.File` is deprecated. Use `cog.Path` for all new models. `cog.File` represents a file handle rather than a path on disk, which makes it harder to work with in practice. Existing models using `cog.File` will continue to work, but new code should use `cog.Path`.

## Next steps

- See the [prediction interface reference](../python.md#path) for full `cog.Path` documentation.
- See [How to stream output](streaming.md) to stream files as they are generated.
- See the [HTTP API reference](../http.md#file-uploads) for the file upload protocol.
