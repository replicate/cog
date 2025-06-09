import os
from typing import Optional
from cog import BaseModel, Input, Path as CogPath, Secret

# We return a path to our trained adapter weights
class TrainingOutput(BaseModel):
    weights: CogPath

def train(
    # Basic input
    some_input: str = Input(
        description="A basic string input to satisfy minimum requirements.",
        default="default value",
    ),
    # String input with None default (problematic)
    hf_repo_id: Optional[str] = Input(
        description="String with None default - this causes issues.",
        default=None,
    ),
    # Secret with None default (problematic)
    hf_token: Optional[Secret] = Input(
        description="Secret with None default - this also causes issues.",
        default=None,
    ),
    # String input with empty string default (works)
    working_repo_id: str = Input(
        description="String with empty string default - this works.",
        default="",
    ),
    # Secret with empty string default (works)
    working_token: Secret = Input(
        description="Secret with empty string default - this works.",
        default="",
    ),
) -> TrainingOutput:
    """
    Minimal example to demonstrate issues with Secret inputs.
    """
    print("\n=== Minimal Cog Secret Test ===")
    print(f"cog version: {os.environ.get('COG_VERSION', 'unknown')}")
    
    # Inputs with None defaults
    print("\n-- Inputs with None defaults (problematic) --")
    print(f"hf_repo_id: {hf_repo_id}")
    if hf_token:
        print(f"hf_token: [PROVIDED]")
        try:
            value = hf_token.get_secret_value()
            print("Secret access successful")
        except Exception as e:
            print(f"Error accessing secret: {e}")
    else:
        print("hf_token: None")
    
    # Inputs with empty string defaults
    print("\n-- Inputs with empty string defaults (works) --")
    print(f"working_repo_id: {working_repo_id if working_repo_id else '(empty)'}")
    if working_token and working_token.get_secret_value():
        print(f"working_token: [PROVIDED]")
        try:
            value = working_token.get_secret_value()
            print("Secret access successful")
        except Exception as e:
            print(f"Error accessing secret: {e}")
    else:
        print("working_token: (empty)")
    
    # Create a dummy output file
    output_path = "dummy_output.txt"
    with open(output_path, "w") as f:
        f.write("This is a dummy output file.")
    
    print("\n=== Test Complete ===")
    
    # Return the dummy output path
    return TrainingOutput(weights=CogPath(output_path))
