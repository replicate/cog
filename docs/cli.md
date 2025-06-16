# CLI

Cog provides a command-line interface for building, running, and deploying machine learning models.

## Overview

The Cog CLI follows this general pattern:

```
cog [global-options] <command> [command-options] [arguments]
```

For help with any command, use the `--help` flag:

```bash
cog --help
cog build --help
```

## Global Options

These options are available for all commands:

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--debug` | bool | false | Show debugging output |
| `--version` | bool | false | Show version of Cog |

## Commands

### cog init

Initialize a new Cog project in the current directory.

```
cog init
```

This command creates:
- `cog.yaml` - Configuration file defining the environment
- `predict.py` - Python file with a basic prediction model template
- `requirements.txt` - Python dependencies file

**Examples:**

```bash
# Initialize a new project
cog init

# The created files provide a starting template
ls
# cog.yaml  predict.py  requirements.txt
```

### cog build

Build a Docker image from a `cog.yaml` configuration file.

```
cog build [options]
```

**Flags:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-t, --tag` | string | | A name for the built image in the form 'repository:tag' |
| `--progress` | string | auto | Set type of build progress output: 'auto', 'tty', or 'plain' |
| `--secret` | string[] | | Secrets to pass to the build environment in the form 'id=foo,src=/path/to/file' |
| `--no-cache` | bool | false | Do not use cache when building the image |
| `--separate-weights` | bool | false | Separate model weights from code in image layers |
| `--openapi-schema` | string | | Load OpenAPI schema from a file |
| `--use-cuda-base-image` | string | auto | Use Nvidia CUDA base image: 'true', 'false', or 'auto' |
| `--use-cog-base-image` | bool | true | Use pre-built Cog base image for faster cold boots |
| `-f` | string | cog.yaml | The name of the config file |

**Examples:**

```bash
# Build with default settings
cog build

# Build with a custom tag
cog build -t my-model:latest

# Build without cache
cog build --no-cache

# Build with separated weights for faster deploys
cog build --separate-weights -t my-model:v1

# Build without CUDA for smaller images (non-GPU models)
cog build --use-cuda-base-image=false
```

### cog predict

Run a prediction on a model.

```
cog predict [image] [options]
```

If an image is specified, it runs predictions on that Docker image. Otherwise, it builds the model in the current directory and runs predictions on it.

**Flags:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-i, --input` | string[] | | Inputs in the form name=value. Use @filename to read from a file |
| `-o, --output` | string | | Output path |
| `-e, --env` | string[] | | Environment variables in the form name=value |
| `--json` | string | | Pass inputs as JSON object from file (@inputs.json) or stdin (@-) |
| `--use-replicate-token` | bool | false | Pass REPLICATE_API_TOKEN from local environment |
| `--setup-timeout` | uint32 | 300 | Timeout for container setup in seconds |
| `--gpus` | string | | GPU devices to add to the container |
| `--use-cuda-base-image` | string | auto | Use Nvidia CUDA base image |
| `--use-cog-base-image` | bool | true | Use pre-built Cog base image |
| `--progress` | string | auto | Set type of build progress output |
| `-f` | string | cog.yaml | The name of the config file |

**Examples:**

```bash
# Run prediction with inputs
cog predict -i image=@input.jpg -i scale=2

# Run prediction with output path
cog predict -i image=@photo.png -o output.png

# Run prediction with JSON input from file
echo '{"image": "@input.jpg", "scale": 2}' > inputs.json
cog predict --json @inputs.json

# Run prediction with JSON input from stdin
echo '{"image": "@input.jpg", "scale": 2}' | cog predict --json @-

# Run prediction on specific image
cog predict my-model:latest -i text="Hello world"

# Run with environment variables
cog predict -e API_KEY=secret -i prompt="Generate text"

# Run with specific GPU
cog predict --gpus 0 -i image=@input.jpg
```

### cog run

Run a command inside a Docker environment defined by Cog.

```
cog run [options] <command> [args...]
```

**Flags:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-p, --publish` | string[] | | Publish a container's port to the host (e.g., -p 8000) |
| `-e, --env` | string[] | | Environment variables in the form name=value |
| `--gpus` | string | | GPU devices to add to the container |
| `--progress` | string | auto | Set type of build progress output |
| `--use-cuda-base-image` | string | auto | Use Nvidia CUDA base image |
| `--use-cog-base-image` | bool | true | Use pre-built Cog base image |
| `-f` | string | cog.yaml | The name of the config file |

**Examples:**

```bash
# Run Python interpreter
cog run python

# Run a Python script
cog run python train.py

# Run with environment variables
cog run -e API_KEY=secret python script.py

# Run with published ports
cog run -p 8888 jupyter notebook

# Run with GPU access
cog run --gpus all python gpu_test.py

# Run bash commands
cog run ls -la
cog run bash -c "echo Hello && python --version"
```

### cog serve

Run the cog HTTP server locally.

```
cog serve [options]
```

Generates and runs an HTTP server based on the model's declared inputs and outputs.

**Flags:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-p, --port` | int | 8393 | Port on which to listen |
| `--gpus` | string | | GPU devices to add to the container |
| `--progress` | string | auto | Set type of build progress output |
| `--use-cuda-base-image` | string | auto | Use Nvidia CUDA base image |
| `--use-cog-base-image` | bool | true | Use pre-built Cog base image |
| `-f` | string | cog.yaml | The name of the config file |

**Examples:**

```bash
# Start server on default port
cog serve

# Start server on custom port
cog serve -p 5000

# Start server with GPU
cog serve --gpus all

# Test the server
curl http://localhost:8393/predictions -X POST \
  -H 'Content-Type: application/json' \
  -d '{"input": {"text": "Hello"}}'
```

### cog push

Build and push a model to a Docker registry.

```
cog push [IMAGE]
```

**Flags:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--secret` | string[] | | Secrets to pass to the build environment |
| `--no-cache` | bool | false | Do not use cache when building |
| `--separate-weights` | bool | false | Separate model weights from code |
| `--openapi-schema` | string | | Load OpenAPI schema from a file |
| `--use-cuda-base-image` | string | auto | Use Nvidia CUDA base image |
| `--use-cog-base-image` | bool | true | Use pre-built Cog base image |
| `--progress` | string | auto | Set type of build progress output |
| `-f` | string | cog.yaml | The name of the config file |

**Examples:**

```bash
# Push to Replicate
cog push r8.im/username/model-name

# Push with separated weights
cog push r8.im/username/model-name --separate-weights

# Push without cache
cog push r8.im/username/model-name --no-cache
```

### cog login

Log in to Replicate Docker registry.

```
cog login [options]
```

**Flags:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--token-stdin` | bool | false | Pass login token on stdin instead of opening browser |

**Examples:**

```bash
# Interactive login (opens browser)
cog login

# Login with token
echo $REPLICATE_API_TOKEN | cog login --token-stdin
```

### cog migrate

Run a migration to update project to newer Cog version.

```
cog migrate [options]
```

**Flags:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-y` | bool | false | Disable interaction and automatically accept changes |
| `-f` | string | cog.yaml | The name of the config file |

**Examples:**

```bash
# Run migration interactively
cog migrate

# Run migration automatically accepting all changes
cog migrate -y
```

### cog debug

Generate a Dockerfile from cog configuration.

```
cog debug [options]
```

**Flags:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--image-name` | string | | The image name for the generated Dockerfile |
| `--separate-weights` | bool | false | Separate model weights from code |
| `--use-cuda-base-image` | string | auto | Use Nvidia CUDA base image |
| `--use-cog-base-image` | bool | true | Use pre-built Cog base image |
| `-f` | string | cog.yaml | The name of the config file |

**Examples:**

```bash
# Generate Dockerfile to stdout
cog debug

# Generate Dockerfile with custom image name
cog debug --image-name my-model:debug
```

## Common Workflows

### Basic Model Development

```bash
# 1. Initialize a new project
cog init

# 2. Edit cog.yaml and predict.py to define your model

# 3. Test predictions locally
cog predict -i input_image=@photo.jpg

# 4. Build and push to registry
cog push r8.im/username/my-model
```

### Using JSON Inputs

The `--json` flag for `cog predict` allows passing complex inputs as JSON:

```bash
# From file
cat > inputs.json << EOF
{
  "prompt": "A beautiful sunset",
  "num_outputs": 4,
  "guidance_scale": 7.5
}
EOF
cog predict --json @inputs.json

# From stdin
echo '{"prompt": "A cat", "seed": 42}' | cog predict --json @-

# With local file paths (automatically converted to base64)
echo '{"image": "@input.jpg", "scale": 2}' | cog predict --json @-
```

### Working with GPUs

```bash
# Use all available GPUs
cog run --gpus all python train.py

# Use specific GPU
cog predict --gpus 0 -i image=@input.jpg

# Use multiple specific GPUs
cog run --gpus '"device=0,1"' python multi_gpu_train.py
```

### Environment Variables

```bash
# Pass environment variables to predict
cog predict -e API_KEY=$MY_API_KEY -i prompt="Hello"

# Pass Replicate API token
export REPLICATE_API_TOKEN=your_token
cog predict --use-replicate-token -i prompt="Hello"

# Multiple environment variables
cog run -e CUDA_VISIBLE_DEVICES=0 -e BATCH_SIZE=32 python train.py
```
