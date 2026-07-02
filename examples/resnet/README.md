# resnet

This model tells you what's in an image. It's configured as a GPU example in `cog.yaml`.

It uses ResNet50 with ImageNet weights from torchvision. Torchvision fetches and
caches the checkpoint the first time the model starts, so startup requires network
access unless the checkpoint is already cached. Takes an image, returns the top-3
ImageNet classes.

## Usage

First, make sure you've got the [latest version of
Cog](https://github.com/replicate/cog#install) installed.

Then run predictions on the model:

```sh
cog predict -i image=@hotdog.png
```
