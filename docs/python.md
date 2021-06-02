# Python reference

The `cog.Model` class defines the standard interface to trained machine learning models. Subclasses of `cog.Model` must implement two functions: `setup()` and `run()`. For example,

```python
import cog

class HelloWorldModel(cog.Model):
    def setup(self):
        self.prefix = "hello "

    @cog.input("text", type=str, help="Text that will get prefixed by 'hello '")
    def predict(self, text):
        return self.prefix + text
```

See the [cog-examples](https://github.com/replicate/cog-examples) repo for more interesting model examples.

## `Model.setup()`

Set up the model for prediction. This is where you load trained models, instantiate data transformations, etc., so as little work as possible has to be done during the actual prediction call.

## `Model.run(**kwargs)`

Run a single prediction. This is where you call the model that was loaded during `setup()`, but you may also want to add pre- and post-processing code here.

The `run()` function takes an arbitrary list of named arguments, where each argument name must correspond to a `@cog.input()` annotation.

`run()` can output strings, numbers, `pathlib.Path` objects, or lists or dicts of those types. We are working on support for other types of output, but for now we recommend using base-64 encoded strings or `pathlib.Path`s for more complex outputs.

### Returning `pathlib.Path` objects

If the output is a `pathlib.Path` object, that will be returned by the built-in HTTP server as a file download.

To output `pathlib.Path` objects the file needs to exist, which means that you probably need to create a temporary file first. This file will automatically be deleted by Cog after it has been returned. For example:

```python
def predict(self, input):
    output = do_some_processing(input)
    out_path = Path(tempfile.mkdtemp()) / "my-file.txt"
    out_path.write_text(output)
    return out_path
```

## `@cog.input(name, type, default, help, min=None, max=None, options=None)`

The `@cog.input()` annotation describes a single input to the `run()` function. The `name` must correspond to an argument name in `run()`.

`type` can be one of:

- `str`
- `int`
- `float`
- `bool`
- `pathlib.Path`

We are working on support for other types of input, but for now we recommend using base-64 encoded strings or `pathlib.Path`s for more complex inputs.

The `pathlib.Path` input type is used for file inputs.

`default` can be any value, or `None` if the input is optional. If no `default` is set, the input is required.

You can document the input argument using `help`. This documentation is surfaced in `cog show <model-id>`.

Type-specific arguments:
* `min` and `max` can be used with `int` and `float` inputs to constrain their ranges.
* `options` can be used with `str`, `int`, and `float` inputs to limit the set of acceptable values.
