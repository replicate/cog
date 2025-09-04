from cog import BasePredictor
import importlib.metadata

class Predictor(BasePredictor):
    def predict(self) -> str:
        """Test function that verifies downloaded requirements are available"""
        
        try:
            # Get all installed packages and their versions
            packages = []
            for dist in importlib.metadata.distributions():
                packages.append(f"{dist.metadata['name']}=={dist.version}")
            
            # Sort for consistent output
            packages.sort()
            
            # Create output with prompt and all packages listed
            return '\n'.join(packages)
            
        except Exception as e:
            return f"ERROR: Unexpected error - {str(e)}"
