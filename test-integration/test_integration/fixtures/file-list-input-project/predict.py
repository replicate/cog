from cog import BasePredictor, File


class Predictor(BasePredictor):
    def predict(self, files: list[File]) -> str:
        output_parts = []  # Use a list to collect file contents
        for f in files:
            # Assuming file content is in bytes, decode to str before appending
            content = f.read()
            if isinstance(content, bytes):
                # Decode bytes to str assuming UTF-8 encoding; adjust if needed
                content = content.decode('utf-8')
            output_parts.append(content)
        return "\n\n".join(output_parts)
