from cog import BasePredictor, Path

class Predictor(BasePredictor):
   def predict(self, paths: list[Path]) -> str:
       output_parts = []  # Use a list to collect file contents
       for path in paths:
           with open(path) as f:
             output_parts.append(f.read())
       return "".join(output_parts)
