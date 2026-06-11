# Hello, Train 🚂

This example demonstrates how to define a training interface in Cog using the `train:` field in `cog.yaml`.

The training API allows you to define a fine-tuning interface for an existing Cog model, so users of the model can bring their own training data to create derivative fine-tuned models. Real-world examples of this API in use include [fine-tuning SDXL with images](https://replicate.com/blog/fine-tune-sdxl) or [fine-tuning Llama 2 with structured text](https://replicate.com/blog/fine-tune-llama-2).

This simple trainable model takes a string as input and returns a string as output.

**Note:** The `cog train` CLI command is deprecated. Training is still supported via the Replicate API and the `train:` field in `cog.yaml`.

## Usage

Run predictions with:

```console
cog run -i text=world
```
