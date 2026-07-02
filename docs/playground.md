# Playground

`cog playground` opens a browser UI for talking to a running Cog model — a Postman-like tool that reflects your model's inputs and outputs from its OpenAPI schema and lets you run predictions interactively.

It doesn't build an image or run your model. Point it at a model API you're already running — typically [`cog serve`](cli.md#cog-serve) — and it proxies requests to that API.

## Quick start

Start your model's HTTP server in one terminal:

```sh
cog serve -p 8393
```

Open the playground in another:

```sh
cog playground --target http://localhost:8393
```

This serves the UI on a local port and opens it in your browser. You can change the target API from the UI at any time.

## What you can do

- **Schema-driven form.** Inputs render from the model's `/openapi.json` as the appropriate widgets (text, number, boolean, enum, list, file, secret). Optional fields without a default start unchecked so they're omitted.
- **Form or JSON.** Toggle between the generated form and a JSON editor; the two stay in sync.
- **Files by upload or URL.** File inputs accept an uploaded file (sent as a data URI) or a URL, with an inline preview for images, audio, and video.
- **Sync, streaming, or async.** Run modes appear based on what the model supports — streaming (SSE) when the predictor uses `@cog.streaming`, and async via webhooks.
- **Rendered or raw output.** View the rendered result (media, text, JSON) or switch to **Raw** to see exactly what arrived over the wire. A Copy button grabs the whole payload.

## Options

| Flag             | Description                                                                                    |
| ---------------- | ---------------------------------------------------------------------------------------------- |
| `--target`       | Default model API URL (also changeable in the UI). Defaults to `http://localhost:8393`.        |
| `-p, --port`     | Port to listen on. `0` (default) picks a free port.                                            |
| `--host`         | Address to bind. Use `0.0.0.0` to receive webhooks from a containerized model.                 |
| `--webhook-host` | Hostname the model uses to reach the playground for webhooks (default `host.docker.internal`). |
| `--no-open`      | Don't open the browser automatically.                                                          |

## CORS and webhooks

Requests are reverse-proxied through the playground, so the model API doesn't need to send any CORS headers.

[Async predictions](http.md#webhooks) are observed via webhooks (there's no status-polling endpoint), so the playground hosts a webhook sink and relays events to the browser. For this to work against a model running in a container, the playground must be reachable from inside the container:

```sh
cog playground --host 0.0.0.0 --webhook-host host.docker.internal
```

> [!NOTE]
> Sync and streaming predictions work without any of this — the webhook setup is only needed for async runs.

## Remote models

If your model runs on another machine, forward its port over SSH and point the playground at it:

```sh
ssh -L 8393:localhost:5000 user@remote
cog playground --target http://localhost:8393
```

Sync and streaming work over the tunnel. For async/webhooks, run the playground next to the model on the remote and forward only the UI port instead.

See the [CLI reference](cli.md#cog-playground) for the full list of flags.
