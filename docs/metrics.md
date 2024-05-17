# Metrics

Prediction objects have a `metrics` field. This normally includes `predict_time` and `total_time`. Official language models have metrics like `input_token_count`, `output_token_count`, `tokens_per_second`, and `time_to_first_token`. Currently, custom metrics from Cog are ignored when running on Replicate. Official Replicate-published models are the only exception to this. When running outside of Replicate, you can emit custom metrics like this:


```python
import cog
from cog import BasePredictor, Path

class Predictor(BasePredictor):
    def predict(self, width: int, height: int) -> Path:
        """Run a single prediction on the model"""
        cog.emit_metric(name="pixel_count", value=width * height)
```
