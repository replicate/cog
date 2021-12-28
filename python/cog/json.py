import cog
import json

# Based on keepsake.json

# We load numpy but not torch or tensorflow because numpy loads very fast and
# they're probably using it anyway
# fmt: off
try:
    import numpy as np  # type: ignore
    has_numpy = True
except ImportError:
    has_numpy = False
# fmt: on

# Tensorflow takes a solid 10 seconds to import on a modern Macbook Pro, so instead of importing,
# do this instead
def _is_tensorflow_tensor(obj):
    # e.g. __module__='tensorflow.python.framework.ops', __name__='EagerTensor'
    return (
        obj.__class__.__module__.split(".")[0] == "tensorflow"
        and "Tensor" in obj.__class__.__name__
    )


def _is_torch_tensor(obj):
    return (obj.__class__.__module__, obj.__class__.__name__) == ("torch", "Tensor")


class CustomJSONEncoder(json.JSONEncoder):
    def default(self, obj):
        if has_numpy:
            if isinstance(obj, np.integer):
                return int(obj)
            elif isinstance(obj, np.floating):
                return float(obj)
            elif isinstance(obj, np.ndarray):
                return obj.tolist()
        if isinstance(obj, cog.File):
            return cog.File.encode(obj)
        elif isinstance(obj, cog.Path):
            return cog.Path.encode(obj)
        elif _is_torch_tensor(obj):
            return obj.detach().tolist()
        elif _is_tensorflow_tensor(obj):
            return obj.numpy().tolist()
        return json.JSONEncoder.default(self, obj)


def to_json(obj):
    return json.dumps(obj, cls=CustomJSONEncoder)
