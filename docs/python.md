# Python reference

The `cog.Model` class defines the standard interface to trained machine learning models. Subclasses of `cog.Model` must implement two functions: `setup()` and `run()`. For example,

```
import cog

class HelloWorldModel(cog.Model):
    def setup(self):
        self.prefix = "hello "

    @cog.input("text", type=str, help="Text that will get prefixed by 'hello '")
    def run(self, text):
        return self.prefix + text
```

See the [cog-examples](https://github.com/replicate/cog-examples) repo for more interesting model examples.

## `Model.setup()`

Set up the model for inference. This is where you load trained models, instantiate data transformations, etc., so that as little work as possible has to be done during the actual inference call.

## `Model.run(**kwargs)`

Run a single inference. This is where you call the model that was loaded during `setup()`, but you may also want to add pre- and post-processing code here.

The `run()` function takes an arbitrary list of named arguments, where each argument name must correspond to a `@cog.input()` annotation.

`run()` can output strings, numbers, `pathlib.Path` objects, or lists or dicts of those types. We are working on supporting other types of output, but for now we recommend base-64 encoding more complex outputs.

If the output is a `pathlib.Path` object, that will be returned by the built-in HTTP server as a file download.

## `@cog.input(name, type, default, help)`

The `@cog.input()` annotation describe a single input to the `run()` function. The `name` must correspond to an argument name in `run()`.

`type` can be one of:
* `str`
* `int`
* `float`
* `bool`
* `pathlib.Path`

We are working on supporting other types of input, but for now we recommend using base-64 encoded strings for more complex inputs.

The `pathlib.Path` input type is used for file inputs.

`default` can be any value, or `None` if the input is optional. If no `default` is set, the input is required.

You can document the input argument using `help`. This documentation is surfaced in `cog show <model-id>`.
