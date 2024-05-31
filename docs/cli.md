# Cog CLI reference


<!-- Do not manually edit this file! It is auto-generated from Go source code. -->


This document defines the command-line interface for Cog.


## `cog init`

Scaffold a new Cog model

### Synopsis

This command sets up a new Cog project in the current directory, with files to get you started:

- cog.yaml, for definining Python and system-level dependencies
- predict.py, for defining the Prediction API for your model
- .dockerignore, to keep large unneeded files out of your published model
- .github/workflows/push.yaml, a GitHub Actions workflow to package and push your model

```
cog init [flags]
```

### Examples

```
mkdir my-model && cd my-model && cog init
```

### Options

```
  -h, --help   help for init
```

### Options inherited from parent commands

```
      --debug     Show debugging output
      --version   Show version of Cog
```


## `cog build`

Build an image from cog.yaml

### Synopsis

This command builds a Docker image from your project's cog.yaml.

This bakes your model's code, the trained weights, and the Docker environment 
into a Docker image which can serve predictions with an HTTP server, and can be 
deployed to anywhere that Docker runs to serve real-time predictions.

```
cog build [flags]
```

### Options

```
  -h, --help                         help for build
      --no-cache                     Do not use cache when building the image
      --openapi-schema string        Load OpenAPI schema from a file
      --progress string              Set type of build progress output, 'auto' (default), 'tty' or 'plain' (default "auto")
      --secret stringArray           Secrets to pass to the build environment in the form 'id=foo,src=/path/to/file'
      --separate-weights             Separate model weights from code in image layers
  -t, --tag string                   A name for the built image in the form 'repository:tag'
      --use-cuda-base-image string   Use Nvidia CUDA base image, 'true' (default) or 'false' (use python base image). False results in a smaller image but may cause problems for non-torch projects (default "auto")
```

### Options inherited from parent commands

```
      --debug     Show debugging output
      --version   Show version of Cog
```


## `cog run`

Run a command inside a Docker environment

### Synopsis

Run a command inside a Docker environment.

The command will be run with the current directory mounted as a volume.

Use commands like "cog run bash" or "cog run python" to access your 
model's runtime environment, so you can interact with it in a Python 
shell, install system dependencies, etc.

```
cog run <command> [arg...] [flags]
```

### Examples

```
# Run Python in your container using the Python version you set in cog.yaml
cog run python

# Access an interactive shell inside your container
cog run bash
```

### Options

```
  -e, --env stringArray              Environment variables, in the form name=value
      --gpus docker run --gpus       GPU devices to add to the container, in the same format as docker run --gpus.
  -h, --help                         help for run
      --progress string              Set type of build progress output, 'auto' (default), 'tty' or 'plain' (default "auto")
  -p, --publish stringArray          Publish a container's port to the host, e.g. -p 8000
      --use-cuda-base-image string   Use Nvidia CUDA base image, 'true' (default) or 'false' (use python base image). False results in a smaller image but may cause problems for non-torch projects (default "auto")
```

### Options inherited from parent commands

```
      --debug     Show debugging output
      --version   Show version of Cog
```


## `cog predict`

Run a prediction

### Synopsis

This command runs a prediction.

If 'image' is passed, it will run the prediction on that Docker image.
It must be an image that has been built by Cog.

Otherwise, it will build the model in the current directory and run
the prediction on that.

```
cog predict [image] [flags]
```

### Examples

```
cog predict -i mask_image=@my_mask.png -i meaning_of_life=42
```

### Options

```
  -e, --env stringArray              Environment variables, in the form name=value
      --gpus docker run --gpus       GPU devices to add to the container, in the same format as docker run --gpus.
  -h, --help                         help for predict
  -i, --input stringArray            Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg
  -o, --output string                Output path
      --progress string              Set type of build progress output, 'auto' (default), 'tty' or 'plain' (default "auto")
      --use-cuda-base-image string   Use Nvidia CUDA base image, 'true' (default) or 'false' (use python base image). False results in a smaller image but may cause problems for non-torch projects (default "auto")
```

### Options inherited from parent commands

```
      --debug     Show debugging output
      --version   Show version of Cog
```


## `cog login`

Log in to the Replicate Docker registry

### Synopsis

Log in to the Replicate Docker registry.

This will allow you to push and pull Docker images from the Replicate registry.

```
cog login [flags]
```

### Examples

```
# log in interactively via web browser
cog login

# pipe token from environment variable
echo $REPLICATE_API_TOKEN | cog login --token-stdin

# log in to a custom registry
cog login --registry=my-custom-docker-registry.com
```

### Options

```
  -h, --help          help for login
      --token-stdin   Pass login token on stdin instead of opening a browser. You can find your Replicate login token at https://replicate.com/auth/token
```

### Options inherited from parent commands

```
      --debug     Show debugging output
      --version   Show version of Cog
```


## `cog push`

Build and push model in current directory to a Docker registry

```
cog push [IMAGE] [flags]
```

### Examples

```
cog push r8.im/your-username/hotdog-detector
```

### Options

```
  -h, --help                         help for push
      --no-cache                     Do not use cache when building the image
      --openapi-schema string        Load OpenAPI schema from a file
      --progress string              Set type of build progress output, 'auto' (default), 'tty' or 'plain' (default "auto")
      --secret stringArray           Secrets to pass to the build environment in the form 'id=foo,src=/path/to/file'
      --separate-weights             Separate model weights from code in image layers
      --use-cuda-base-image string   Use Nvidia CUDA base image, 'true' (default) or 'false' (use python base image). False results in a smaller image but may cause problems for non-torch projects (default "auto")
```

### Options inherited from parent commands

```
      --debug     Show debugging output
      --version   Show version of Cog
```
