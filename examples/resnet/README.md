# resnet

This model tells you what's in an image. It's a good example of a deep
learning model that's small enough to run without a GPU if you're demoing it.

It uses ResNet50 with the ImageNet weights that ship with torchvision, so
there are no weight files to download or import -- torchvision fetches them
the first time the model runs. Takes an image, returns the top-3 ImageNet
classes.

## Usage

First, make sure you've got the [latest version of
Cog](https://github.com/replicate/cog#install) installed.

Then run predictions on the model:

```sh
cog predict -i image=@hotdog.png
```
