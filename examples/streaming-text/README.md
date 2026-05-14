# examples/streaming-text

Streaming text generation with `HuggingFaceTB/SmolLM2-135M-Instruct`.

This example shows how a Cog predictor can yield text chunks as a model generates them, and how to consume those chunks from the Server-Sent Events stream endpoint.

## Run a normal prediction

From this directory:

```sh
cog predict -i prompt="Write a short haiku about databases"
```

This returns the final accumulated output after the prediction completes.

## Stream output over HTTP

Start the server:

```sh
cog serve
```

Create an async prediction with a fixed ID:

```sh
curl -s -X PUT http://localhost:5000/predictions/streaming-demo \
  -H 'Content-Type: application/json' \
  -H 'Prefer: respond-async' \
  -d '{"input":{"prompt":"Write a short haiku about databases","max_new_tokens":96}}'
```

Then subscribe to its stream:

```sh
curl -N -H 'Accept: text/event-stream' \
  http://localhost:5000/predictions/streaming-demo/stream
```

The response includes `output` events as chunks are generated, followed by a `completed` event:

```text
event: output
data: {"chunk":"Silent","index":0}

event: output
data: {"chunk":" rows","index":1}

event: completed
data: {"id":"streaming-demo","status":"succeeded",...}
```

## How it works

`predict.py` returns `Iterator[str]`. Each `yield` becomes one streamed output chunk. The example uses Hugging Face `TextIteratorStreamer` to receive generated text from `model.generate()` while generation is still running.

The normal prediction response still contains the accumulated output for compatibility. The stream endpoint is useful when clients want to display tokens as they arrive.
