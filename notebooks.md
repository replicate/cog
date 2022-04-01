# Notebooks

Cog plays nicely with Jupyter notebooks.

## Run a notebook

Cog can run notebooks in the environment you've defined in [`cog.yaml`](yaml.md) with the following command:

```sh
cog run -p 8888 jupyter notebook --allow-root --ip=0.0.0.0
```

## Use notebook code in your predictor

You can also import a notebook into your Cog [Predictor](python.md) file.

First, export your notebook to a Python file:

```sh
jupyter nbconvert --to script my_notebook.ipynb # create my_notebook.py
```

Then import the exported Python script into your `predict.py` file. Any functions or variables defined in your notebook will be available to your predictor:

```python
from cog import BasePredictor, Input

import my_notebook

class Predictor(BasePredictor):
    def predict(self, prompt: str = Input(description="string prompt")) -> str:
      output = my_notebook.do_stuff(prompt)
      return output
```
