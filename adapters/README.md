# Cog adapters

This is a proposed design of what a basic _build adapter_ can
look like.  A build adapter is a script that runs on the Cog server
after a Cog model has been uploaded and tested.

The purpose of a build adapter is to build a Cog model for arbitrary
targets, such as prediction services like Sagemaker and Seldon Core,
but also potentially Core ML models, ONNX, etc. The adapter's built
artifact URI is then stored alongside the model in the Cog database.

In the current prototype implementation, each adapter is executed on
each model. This will probably change such that the user can declare
which adapters it want to execute on `cog build`. We will probably
also make it possible to execute adapters to create new artifacts
post-hoc, after the model has already been uploaded.

An adapter is defined by a name and an executable script file,
described in cog-adapter.yaml. Any number of directories containing
cog-adapter.yaml can be passed to `cog server
--adapter=/path/to/adapter/dir`.

The adapter script itself can be written in any language. The server
runs the adapter in a subprocess once per each uploaded model, so the
script needs to be stateless.

## Interface

Adapter scripts are executed in the Cog model's root directory.

The server communicates with the adapter script by setting a number of
environment variables:

* `COG_MODEL_PYTHON_MODULE` - The module containing the cog.Model subclass, e.g. "jazzcomposer.infer"
* `COG_MODEL_CLASS` - The name of the cog.Model model class, e.g. "MyModel"
* `COG_CPU_IMAGE` - The built CPU Docker image, e.g. "my-registry.pkg.dev/andreas/jazzcomposer:a1b2c3"
* `COG_DOCKER_REGISTRY` - The Docker registry, e.g. "my-registry.pkg.dev"
* `COG_HAS_DOCKER_REGISTRY` - Set to "true" if the server was started with a registry, otherwise "false"
* `COG_REPO_USER` - The user of the repo, e.g. "andreas"
* `COG_REPO_NAME` - The name of the repo, e.g. "jazzcomposer"
* `COG_MODEL_ID` - The model ID, e.g. "f4b7be655ccf029e36f1e7b941cbaf87cfb9ca90"

The adapter script must output a single line to stdout, containing the
URI of the created artifact. The adapter script is responsible for
uploading the artifact to whatever storage it is hosted on.

The adapter can output any number of lines to stderr, which will sent
as logging to `cog build`.

## Design questions

* Should adapters be tested automatically as the CPU docker image is, and how should that be implemented?
* Are there higher level abstractions than an executable script and environment variables that would make the adapter API more ergonomic?
* Are there cases where adapters need to be stateful?
* Are there cases where adapters need to output more than one artifact URI?
