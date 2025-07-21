# Minimal Source Copy Fixture

## Purpose
Tests that source files are correctly copied from the build context to the final runtime image.

## Configuration
- **Python Version**: 3.11
- **Dependencies**: None (tests source copy without dependency complications)
- **Source Files**: Single `predict.py` file with a simple predictor

## Expected Build Result
The built image should contain:
- `/app/predict.py` with the exact content from the source
- Python 3.11 runtime available
- No additional dependencies installed

## Test Assertions
Integration tests should verify:
1. `/app/predict.py` file exists in the runtime image
2. File content matches the source exactly
3. Python can import and execute the predictor
4. No unexpected files are present in `/app/`

## Model Behavior
The predictor simply echoes the input string, making it easy to verify the model works without complex dependencies.