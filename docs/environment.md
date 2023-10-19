# Environment Variables

This guide provides a list of environment variables that can be set that impact the execution of Cog. This assumes that you have already followed the [main steps to set up Cog](https://github.com/replicate/cog/blob/main/docs/getting-started.md) and have a general familiarity with how to run Cog.


## General

This section lists the relevant environment variables for general execution of Cog.

### `CGO_ENABLED`
This variable determines whether the usage of cgo should be enabled or disabled. cgo is a subsystem in Go that enables the programmer to invoke C code from Go packages.

This can be set to either 0 or 1 to enable/disable cgo. By default, it is set to 0 in order to create statically linked binaries that can help with the portability of containers by ensuring that the binary is not reliant on shared libraries provided with a source image.

### `COG_NO_UPDATE_CHECK`
This determines whether there should be an update check or not. An update check will display an update message if an update is available and will check for a new update in the background. The result of that check will then be displayed the next time the user runs Cog.

This can be either set/unset in order to disable/enable the update checks. By default, it is not set.

### `LOG_FORMAT`
This determines what format to output the logs. Specifically, if set to "development", then it will switch to a human-friendly log output.

This can be set to development or entirely omitted. By default, the log format will be left unset and thus not have the human-friendly output to the logs.


## Model

This section lists the relevant environment variables for models.

### `COG_WEIGHTS`
This specifies the URLs to the paths or files for any model weights that will be used to prepare the model.

This can be set to a string that specifies the path/file. By default, it is not set to anything.

### `HOSTNAME`
This specifies the hostname for the model in the span attributes.

This can be set to a string that specifies the hostname for the model. By default, it is not set to anything.

### `COG_MODEL_ID`
This specifies the model id for the model in the span attributes. Model id is a unique ID for the Cog model that is used to label setup logs (this is subject to change soon with logging changes in 0.3.0).

This can be set to a string that specifies the model id for the model. By default, it is not set to anything.

### `COG_MODEL_NAME`
This specifies the model name for the model in the span attributes.

This can be set to a string that specifies the model name for the model. By default, it is not set to anything.

### `COG_USERNAME`
This specifies the username for the model in the span attributes.

This can be set to a string that specifies the username for the model. By default, it is not set to anything.

### `COG_MODEL_VERSION`
This specifies the version for the model in the span attributes.

This can be set to a string that specifies the version for the model. By default, the program will assume the value of the prediction's version to be the same for the model version if there is a prediction, otherwise it will be set to nothing.

### `COG_HARDWARE`
This specifies the hardware for the model in the span attributes.

This can be set to a string that specifies the hardware for the model. By default, it is not set to anything.

### `COG_DOCKER_IMAGE_URI`
This specifies the docker image URI for the model in the span attributes.

This can be set to a string that specifies the docker image URI for the model. By default, it is not set to anything.


## Server

This section lists the relevant environment variables for running the [HTTP API](https://github.com/replicate/cog/blob/main/docs/http.md) when making predictions.

### `COG_LOG_LEVEL`
This defines what type of messages are reported/displayed when running the HTTP server that makes the cog predictions.

It can be set to info, debug, or warning. By default, the info log level is used if not supplied.

### `PORT`
This defines what port is exposed from the container for the HTTP server to be hosted on.

This can be set to any valid port number. By default, the port number will be set to 5000.

### `COG_THROTTLE_RESPONSE_INTERVAL`
This specifies the duration that the server should wait before sending another response, as handled by the ResponseThrottler.

This can be set to a float, which will represent the number of seconds to wait between responses. By default, this will be set to 0.5.

### `WEBHOOK_AUTH_TOKEN`
This specifies the authentication token (if necessary) in order to be authorized for a webhook call.

This can be set to the string representing your authentication token. By default, this will be set to nothing.

### `KUBERNETES_SERVICE_HOST`
This determines whether or not to run Cog with Kubernetes. Running with Kubernetes will result in Cog setting up probe helpers, which is what kubelets use in Kuberenetes to determine when to restart containers, when containers are ready for traffic, and when container applications have started.

This can be set / unset. By default, it is unset.


## Redis

This section lists the relevant environment variables for Cog's built-in queue worker to process predictions from a [Redis queue](https://github.com/replicate/cog/blob/c896373a0ced5416552871751f028452249fb9c2/docs/redis.md).

### `OTEL_SERVICE_NAME`
This determines whether to enable or disable the usage of OpenTelemetry. OpenTelemetry (OTEL) is an open-source technology used to capture and measure metrics, traces, and logs. It is used for Cog's queue worker.

This can either be set / unset in order to determine whether or not to enable OpenTelemtry. If it is set, then Cog will handle the necessary setup for OpenTelemtry. Otherwise, OpenTelemetry calls will be treated as no-ops. If OpenTelemetry is enabled, the OTLP exporter may also need to be [configured via environment variables](https://opentelemetry-python.readthedocs.io/en/latest/sdk/environment_variables.html).


## Docker Image

### `NVIDIA_DRIVER_CAPABILITIES`
This [controls which Nvidia driver libraries/binaries will be mounted inside the container](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/docker-specialized.html#driver-capabilities). The generated Docker image will set this to  `all` which will mount all Nvidia driver libraries/binaries inside the container beyond the default `utility` and `compute` capabilities.

`graphics`, `video`, and `display` add additional interesting capabilities beyond the default that may be useful for some models running in the container. `graphics` is required for accelerated OpenGL support. `video` is required for accelerated video encoding/decoding. `display` is required for accelerated X11 support.

This is set to `all`, is non-configurable, and is documented here as an environment variable of interest. Setting or changing this during runtime inside the image will have no effect.
