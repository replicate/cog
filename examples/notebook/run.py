from cog import BaseRunner, Input

import my_notebook


class Runner(BaseRunner):
    def setup(self):
        """Prepare the model so multiple predictions run efficiently (optional)"""

    def run(self, name: str = Input(description="name of person to greet")) -> str:
        """Run a single prediction"""

        output = my_notebook.say_hello(name)
        return output
