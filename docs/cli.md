# CLI reference

<!-- This file is auto-generated. Do not edit manually. -->

## `cog`

Containers for machine learning.

To get started, take a look at the documentation:
https://github.com/replicate/cog

**Examples**

```
   To run a command inside a Docker environment defined with Cog:
      $ cog run echo hello world
```

**Options**

```
      --debug     Show debugging output
  -h, --help      help for cog
      --version   Show version of Cog
```
## `cog build`

Build an image from cog.yaml

```
cog build [flags]
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
## `cog init`

Configure your project for use with Cog

```
cog init [flags]
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
## `cog predict`

Run a prediction.

If 'image' is passed, it will run the prediction on that Docker image.
It must be an image that has been built by Cog.

Otherwise, it will build the model in the current directory and run
the prediction on that.

```
cog predict [image] [flags]
```

**Options**

```
  -e, --env stringArray              Environment variables, in the form name=value
  -f, --file string                  The name of the config file. (default "cog.yaml")
      --gpus docker run --gpus       GPU devices to add to the container, in the same format as docker run --gpus.
  -h, --help                         help for predict
  -i, --input stringArray            Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg
      --json string                  Pass inputs as JSON object, read from file (@inputs.json) or via stdin (@-)
  -o, --output string                Output path
      --progress string              Set type of build progress output, 'auto' (default), 'tty', 'plain', or 'quiet' (default "auto")
      --setup-timeout uint32         The timeout for a container to setup (in seconds). (default 300)
      --use-cog-base-image           Use pre-built Cog base image for faster cold boots (default true)
      --use-cuda-base-image string   Use Nvidia CUDA base image, 'true' (default) or 'false' (use python base image). False results in a smaller image but may cause problems for non-torch projects (default "auto")
      --use-replicate-token          Pass REPLICATE_API_TOKEN from local environment into the model context
```
## `cog push`

Build and push model in current directory to a Docker registry

```
cog push [IMAGE] [flags]
```

**Examples**

```
cog push registry.example.com/your-username/model-name
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

Run a command inside a Docker environment

```
cog run <command> [arg...] [flags]
```

**Options**

```
  -e, --env stringArray              Environment variables, in the form name=value
  -f, --file string                  The name of the config file. (default "cog.yaml")
      --gpus docker run --gpus       GPU devices to add to the container, in the same format as docker run --gpus.
  -h, --help                         help for run
      --progress string              Set type of build progress output, 'auto' (default), 'tty', 'plain', or 'quiet' (default "auto")
  -p, --publish stringArray          Publish a container's port to the host, e.g. -p 8000
      --use-cog-base-image           Use pre-built Cog base image for faster cold boots (default true)
      --use-cuda-base-image string   Use Nvidia CUDA base image, 'true' (default) or 'false' (use python base image). False results in a smaller image but may cause problems for non-torch projects (default "auto")
```
## `cog serve`

Run a prediction HTTP server.

Generate and run an HTTP server based on the declared model inputs and outputs.

```
cog serve [flags]
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
