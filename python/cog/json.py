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
    def default(self, o):
        if has_numpy:
            if isinstance(o, np.integer):
                return int(o)
            elif isinstance(o, np.floating):
                return float(o)
            elif isinstance(o, np.ndarray):
                return o.tolist()
        if _is_torch_tensor(o):
            return o.detach().tolist()
        if _is_tensorflow_tensor(o):
            return o.numpy().tolist()
        return json.JSONEncoder.default(self, o)


def to_json(obj):
    return json.dumps(obj, cls=CustomJSONEncoder)
