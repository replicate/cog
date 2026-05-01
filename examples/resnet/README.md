# examples/resnet

ResNet50 image classifier (microsoft/resnet-50 from HuggingFace) packaged
with v1 managed weights. Takes an image, returns top-3 ImageNet classes.

## Usage

Import weights from HuggingFace and generate the lockfile:

```sh
cd examples/resnet
cog weights import
```

Run a prediction locally (weights are bind-mounted):

```sh
cog predict -i image=@hotdog.png
```

Build and push to a registry:

```sh
cog push <registry>/resnet
```
