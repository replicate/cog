# hello-replicate

An example Cog model that demonstrates `cog.Secret` inputs by calling the
[Replicate API](https://replicate.com/docs) from inside a prediction.

Given an input image, the model:

1. Sends the image to `anthropic/claude-4-sonnet` to generate a detailed prompt
   describing it.
2. Feeds that prompt to `black-forest-labs/flux-dev` to re-create the image.
3. Returns the generated image.

## Secrets

The Replicate API token is declared as a `cog.Secret` input:

```python
from cog import Input, Secret

def run(
    replicate_api_token: Secret = Input(
        description="Replicate API token used to call other models",
    ),
) -> Path:
    client = Client(api_token=replicate_api_token.get_secret_value())
    ...
```

`cog.Secret` redacts its value in logs and string representations. Read the
underlying value with `get_secret_value()`.

## Run it

Avoid passing the token literally on the command line, since it can leak
through your shell history and process listings. Instead, read it from an
environment variable:

```sh
export REPLICATE_API_TOKEN=r8_...   # set once, ideally via a secrets manager / not inline in shared shells
cog predict -i image=@cat.png -i replicate_api_token="$REPLICATE_API_TOKEN"
```

You can also read the token from a file (for example
`-i replicate_api_token="$(cat token.txt)"`) if that fits your workflow better.

> **Note:** `cog.Secret` redacts the value in model logs and string
> representations, but it cannot protect a secret that is already exposed by
> your own shell history, environment, or process listing. Keeping the token
> out of those places is your responsibility.
