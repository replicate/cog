# CLI reference

<!-- This file is auto-generated. Do not edit manually. -->

## `cog`

Containers for machine learning.

To get started, take a look at the documentation:
https://github.com/replicate/cog

**Examples**

```
   To run a prediction:
      $ cog run -i prompt="hello world"

   To run a command inside the Docker environment:
      $ cog exec python
```

**Options**

```
      --debug     Show debugging output
  -h, --help      help for cog
      --version   Show version of Cog
```
## `cog build`

Build a Docker image from the cog.yaml in the current directory.

The generated image contains your model code, dependencies, and the Cog
runtime. It can be run locally with 'cog run' or pushed to a registry
with 'cog push'.

```
cog build [flags]
```

**Examples**

```
  # Build with default settings
  cog build

  # Build and tag the image
  cog build -t my-model:latest

  # Build without using the cache
  cog build --no-cache

  # Build with model weights in a separate layer
  cog build --separate-weights -t my-model:v1
```

**Options**

```
  -f, --file string                  The name of the config file. (default "cog.yaml")
  -h, --help                         help for build
      --no-cache                     Do not use cache when building the image
      --openapi-schema string        Load OpenAPI schema from a file
      --progress string              Set type of build progress output, 'auto' (default), 'tty', 'plain', or 'quiet' (default "auto")
      --secret stringArray           Secrets to pass to the build environment in the form 'id=foo,src=/path/to/file'
      --separate-weights             Separate model weights from code in image layers
  -t, --tag string                   A name for the built image in the form 'repository:tag'
      --use-cog-base-image           Use pre-built Cog base image for faster cold boots (default true)
      --use-cuda-base-image string   Use Nvidia CUDA base image, 'true' (default) or 'false' (use python base image). False results in a smaller image but may cause problems for non-torch projects (default "auto")
```
## `cog exec`

Run a command inside a Docker environment defined by cog.yaml.

Cog builds a temporary image from your cog.yaml configuration and runs the
given command inside it. This is useful for debugging, running scripts, or
exploring the environment your model will run in.

```
cog exec <command> [arg...] [flags]
```

**Examples**

```
  # Open a Python interpreter inside the model environment
  cog exec python

  # Run a script
  cog exec python train.py

  # Run with environment variables
  cog exec -e HUGGING_FACE_HUB_TOKEN=abc123 python download.py

  # Expose a port (e.g. for Jupyter)
  cog exec -p 8888 jupyter notebook
```

**Options**

```
  -e, --env stringArray              Environment variables, in the form name=value
  -f, --file string                  The name of the config file. (default "cog.yaml")
      --gpus docker run --gpus       GPU devices to add to the container, in the same format as docker run --gpus.
  -h, --help                         help for exec
      --progress string              Set type of build progress output, 'auto' (default), 'tty', 'plain', or 'quiet' (default "auto")
  -p, --publish stringArray          Publish a container's port to the host, e.g. -p 8000
      --use-cog-base-image           Use pre-built Cog base image for faster cold boots (default true)
      --use-cuda-base-image string   Use Nvidia CUDA base image, 'true' (default) or 'false' (use python base image). False results in a smaller image but may cause problems for non-torch projects (default "auto")
```
## `cog init`

Create a cog.yaml and run.py in the current directory.

These files provide a starting template for defining your model's environment
and prediction interface. Edit them to match your model's requirements.

```
cog init [flags]
```

**Examples**

```
  # Set up a new Cog project in the current directory
  cog init
```

**Options**

```
  -h, --help   help for init
```
## `cog login`

Log in to a container registry.

For Replicate's registry (r8.im), this command handles authentication
through Replicate's token-based flow.

For other registries, this command prompts for username and password,
then stores credentials using Docker's credential system.

```
cog login [flags]
```

**Options**

```
  -h, --help          help for login
      --token-stdin   Pass login token on stdin instead of opening a browser. You can find your Replicate login token at https://replicate.com/auth/token
```
## `cog push`

Build a Docker image from cog.yaml and push it to a container registry.

Cog can push to any OCI-compliant registry. When pushing to Replicate's
registry (r8.im), run 'cog login' first to authenticate.

```
cog push [IMAGE] [flags]
```

**Examples**

```
  # Push to Replicate
  cog push r8.im/your-username/my-model

  # Push to any OCI registry
  cog push registry.example.com/your-username/model-name

  # Push with model weights in a separate layer (Replicate only)
  cog push r8.im/your-username/my-model --separate-weights
```

**Options**

```
  -f, --file string                  The name of the config file. (default "cog.yaml")
  -h, --help                         help for push
      --no-cache                     Do not use cache when building the image
      --openapi-schema string        Load OpenAPI schema from a file
      --progress string              Set type of build progress output, 'auto' (default), 'tty', 'plain', or 'quiet' (default "auto")
      --secret stringArray           Secrets to pass to the build environment in the form 'id=foo,src=/path/to/file'
      --separate-weights             Separate model weights from code in image layers
      --use-cog-base-image           Use pre-built Cog base image for faster cold boots (default true)
      --use-cuda-base-image string   Use Nvidia CUDA base image, 'true' (default) or 'false' (use python base image). False results in a smaller image but may cause problems for non-torch projects (default "auto")
```
## `cog run`

Run a prediction.

If 'image' is passed, it will run the prediction on that Docker image.
It must be an image that has been built by Cog.

Otherwise, it will build the model in the current directory and run
the prediction on that.

```
cog run [image] [flags]
```

**Examples**

```
  # Run a prediction with named inputs
  cog run -i prompt="a photo of a cat"

  # Pass a file as input
  cog run -i image=@photo.jpg

  # Save output to a file
  cog run -i image=@input.jpg -o output.png

  # Pass multiple inputs
  cog run -i prompt="sunset" -i width=1024 -i height=768

  # Run against a pre-built image
  cog run r8.im/your-username/my-model -i prompt="hello"

  # Pass inputs as JSON
  echo '{"prompt": "a cat"}' | cog run --json @-
```

**Options**

```
  -e, --env stringArray              Environment variables, in the form name=value
  -f, --file string                  The name of the config file. (default "cog.yaml")
      --gpus docker run --gpus       GPU devices to add to the container, in the same format as docker run --gpus.
  -h, --help                         help for run
  -i, --input stringArray            Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg
      --json string                  Pass inputs as JSON object, read from file (@inputs.json) or via stdin (@-)
  -o, --output string                Output path
      --progress string              Set type of build progress output, 'auto' (default), 'tty', 'plain', or 'quiet' (default "auto")
      --setup-timeout uint32         The timeout for a container to setup (in seconds). (default 300)
      --use-cog-base-image           Use pre-built Cog base image for faster cold boots (default true)
      --use-cuda-base-image string   Use Nvidia CUDA base image, 'true' (default) or 'false' (use python base image). False results in a smaller image but may cause problems for non-torch projects (default "auto")
      --use-replicate-token          Pass REPLICATE_API_TOKEN from local environment into the model context
```
## `cog serve`

Run a prediction HTTP server.

Builds the model and starts an HTTP server that exposes the model's inputs
and outputs as a REST API. Compatible with the Cog HTTP protocol.

```
cog serve [flags]
```

**Examples**

```
  # Start the server on the default port (8393)
  cog serve

  # Start on a custom port
  cog serve -p 5000

  # Test the server
  curl http://localhost:8393/predictions \
    -X POST \
    -H 'Content-Type: application/json' \
    -d '{"input": {"prompt": "a cat"}}'
```

**Options**

```
  -f, --file string                  The name of the config file. (default "cog.yaml")
      --gpus docker run --gpus       GPU devices to add to the container, in the same format as docker run --gpus.
  -h, --help                         help for serve
  -p, --port int                     Port on which to listen (default 8393)
      --progress string              Set type of build progress output, 'auto' (default), 'tty', 'plain', or 'quiet' (default "auto")
      --upload-url string            Upload URL for file outputs (e.g. https://example.com/upload/)
      --use-cog-base-image           Use pre-built Cog base image for faster cold boots (default true)
      --use-cuda-base-image string   Use Nvidia CUDA base image, 'true' (default) or 'false' (use python base image). False results in a smaller image but may cause problems for non-torch projects (default "auto")
```
