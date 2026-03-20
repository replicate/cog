# How to use Jupyter notebooks

This guide shows you how to run Jupyter notebooks inside Cog's Docker environment and how to import notebook code into your predictor.

## Add JupyterLab to your dependencies

Add `jupyterlab` to your `requirements.txt`:

```
jupyterlab
```

Reference it in [`cog.yaml`](../yaml.md):

```yaml
build:
  python_requirements: requirements.txt
```

## Run a notebook

Start JupyterLab inside the Cog environment:

```sh
cog run -p 8888 jupyter lab --allow-root --ip=0.0.0.0
```

This forwards port 8888 from the container to your host machine. Open the URL printed in the terminal to access the notebook interface.

If you prefer a different notebook environment, you can use `cog run` to launch it the same way. For example, to run the classic Jupyter Notebook server:

```sh
cog run -p 8888 jupyter notebook --allow-root --ip=0.0.0.0
```

## Use notebook code in your predictor

To import functions or variables from a notebook into your [predictor](../python.md), first export the notebook to a Python file:

```sh
jupyter nbconvert --to script my_notebook.ipynb  # creates my_notebook.py
```

Then import the exported module in your `predict.py`:

```python
from cog import BasePredictor, Input

import my_notebook

class Predictor(BasePredictor):
    def predict(self, prompt: str = Input(description="string prompt")) -> str:
        output = my_notebook.do_stuff(prompt)
        return output
```

Any functions or variables defined in the notebook will be available to your predictor.
