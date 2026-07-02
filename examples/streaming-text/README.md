# examples/streaming-text

Streaming text generation with `HuggingFaceTB/SmolLM2-135M-Instruct`.

This example shows how a Cog runner can yield text chunks as a model generates them, and how to consume those chunks with Server-Sent Events.

## Run a normal prediction

From this directory:

```sh
cog run -i prompt="Write a short haiku about databases"
```

This returns the final accumulated output after the prediction completes.

## Stream output over HTTP

Start the server:

```sh
cog serve
```

Create a prediction and request an SSE response:

```sh
curl -N -X PUT http://localhost:5000/predictions/streaming-demo \
  -H 'Content-Type: application/json' \
  -H 'Accept: text/event-stream' \
  -d '{"input":{"prompt":"Write a short haiku about databases","max_new_tokens":96}}'
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

`run.py` defines `run() -> Iterator[str]`. Each `yield` becomes one streamed output chunk. The example uses Hugging Face `TextIteratorStreamer` to receive generated text from `model.generate()` while generation is still running.

The normal prediction response still contains the accumulated output for compatibility. Requesting `Accept: text/event-stream` is useful when clients want to display tokens as they arrive.
