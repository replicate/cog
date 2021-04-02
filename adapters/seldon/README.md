# Cog build adapter for Seldon Core

This is a prototype implementation of Cog build adapters. Use it by starting the server with

```
$ cog server --adapter=adapters/seldon [...]
```

When you run `cog build` in a model directory, the server will product Seldon Core models.

```
$ cog build
═══╡ Uploading /home/andreas/cog-examples/hello-world to localhost:8080/andreas/hello-world
═══╡ Building package...
[...]
═══╡ Building adapter target: seldon-core
═══╡ Building Seldon image
═══╡ Pushing Seldon package
═══╡ Successfully built Seldon container
   │ my-registry.pkg.dev/andreas/hello-world:f4b7be655ccf029e36f1e7b941cbaf87cfb9ca90-seldon
═══╡ Inserting into database
Successfully built f4b7be655ccf029e36f1e7b941cbaf87cfb9ca90
```

The artifact is listed in `cog show`:

```
$ cog show f4b7be655ccf029e36f1e7b941cbaf87cfb9ca90
ID:       f4b7be655ccf029e36f1e7b941cbaf87cfb9ca90
Repo:     andreas/hello-world
Created:  9 seconds ago

Inference arguments:
* text  (str)  Text that will get prefixed by 'hello '

Artifacts:
* docker-cpu:   my-registry/andreas/hello-world:d2323d7c5630
* seldon-core:  my-registry/andreas/hello-world:f4b7be655ccf029e36f1e7b941cbaf87cfb9ca90-seldon

Python version: 3.8
```

You can then test the Seldon Core artifact:

```
$ docker run -it -p 9000:9000 my-registry/andreas/hello-world:f4b7be655ccf029e36f1e7b941cbaf87cfb9ca90-seldon
```

And in a different shell:

```
$ curl -X POST -H "Content-Type: application/json" \
    -d '{"data":  {"names": ["text"], "ndarray": ["world"]}}' \
    http://localhost:9000/api/v0.1/predictions

{"meta":{},"strData":"hello world"}
```
