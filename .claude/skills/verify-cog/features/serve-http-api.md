# Serve the HTTP prediction API

`cog serve -p <port>` builds the model, starts the coglet server in a container and publishes it on `127.0.0.1:<port>`. The server exposes the Cog HTTP API: health, synchronous and asynchronous predictions, validation errors, an OpenAPI document, server-sent events for streaming models, cancellation, idempotent PUT and an explicit shutdown.

## Sub-features

- `serve-ready` reports `STARTING` then `READY` on `/health-check` with coglet, SDK and Python versions.
- `serve-predict` answers `POST /predictions` synchronously with `status: succeeded` and the output.
- `serve-validation` answers an invalid body with HTTP 422 and a `detail` list naming each field.
- `serve-openapi` serves `/openapi.json` whose `info.version` is the CLI's version and whose Input schema carries the constraints.
- `serve-sse` streams `start`, `output` and `completed` events for a `@streaming` model with `Accept: text/event-stream` (fixture `stream`).
- `serve-async` answers `Prefer: respond-async` with 202 and `status: starting` (fixture `stream`).
- `serve-cancel` cancels a running prediction by id with 200 (fixture `stream`).
- `serve-put` creates a prediction idempotently with `PUT /predictions/<id>` (fixture `stream`).
- `serve-shutdown` stops the server through `POST /shutdown`, removing the container and releasing the port.

## How to get to it (user POV)

- Run `cog serve -p <port>` in the project directory and open or `curl` `http://127.0.0.1:<port>`.
- Send `POST /predictions`, `PUT /predictions/<id>`, `POST /predictions/<id>/cancel`, `GET /health-check`, `GET /openapi.json`, `POST /shutdown`.
- Add `Prefer: respond-async` or `Accept: text/event-stream` to a prediction request.
- Not covered: `cog serve --playground`, `--host`, `--upload-url`, webhooks.

## Driving it with cogverify

Preconditions:

- `doctor: OK`. For the first group of steps use a run with fixture `echo`; for the group marked `stream` use a second run created with `--fixture stream`.
- Nothing is running for the run yet (starting a second server for the same run is refused).
- Start the server: `$V serve-start`. It prints `READY: http://127.0.0.1:<port> cog pid=<pid> container=<id>`. Re-run the prelude (`eval "$($V env)"`) so `$SERVE_URL` is exported, then `$V doctor` should show `port <port> is published by our container only` and `/health-check status=READY`.

- **Health.** Run `$V tx --exit 0 --stdout '"status":"READY"' --stdout '"python_sdk":"'"$(tr -d '[:space:]' < "$REPO/VERSION.txt")"'"' --stdout '"python":"3.12' serve-http-api--health 'curl -sS "$SERVE_URL/health-check"'`.
- **Predict (echo).** Run `$V tx --exit 0 --stdout '"status":"succeeded"' --stdout '"output":"verify:hi verify:hi"' serve-http-api--predict 'curl -sS -X POST "$SERVE_URL/predictions" -H "Content-Type: application/json" -d "{\"input\":{\"text\":\"hi\"}}"'`.
- **Validation (echo).** Run `$V tx --exit 0 --stdout 'HTTP 422' --stdout '9 is greater than the maximum of 5' --stdout '"loc":\["body","input","text"\]' serve-http-api--validation 'curl -sS -X POST "$SERVE_URL/predictions" -H "Content-Type: application/json" -d "{\"input\":{\"times\":9}}" -w "\nHTTP %{http_code}\n"'`. Both the range error on `times` and the missing `text` are listed.
- **OpenAPI (echo).** Run `$V tx --exit 0 --stdout '"version": "[0-9.]+-dev\+g'"$(git -C "$REPO" rev-parse --short HEAD)"'"' --stdout '"maximum": 5' --stdout "/predictions/\{prediction_id\}/cancel" serve-http-api--openapi 'curl -sS "$SERVE_URL/openapi.json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(json.dumps(d[\"info\"])); print(sorted(d[\"paths\"])); print(json.dumps(d[\"components\"][\"schemas\"][\"Input\"][\"properties\"][\"times\"]))"'`. `info.version` equals the CLI version, which ties the schema to the binary under test.
- **Stream group, SSE.** With the `stream` run's server up, run `$V tx --exit 0 --stdout '^event: start' --stdout '"chunk":"one","index":0' --stdout '"chunk":"three","index":2' --stdout '^event: completed' --stdout '"output":\["one","two","three"\]' serve-http-api--sse 'curl -sN --max-time 30 -X POST "$SERVE_URL/predictions" -H "Content-Type: application/json" -H "Accept: text/event-stream" -d "{\"input\":{\"words\":\"one two three\"}}"'`. Events arrive in order `start`, three `output` (index 0..2), `completed`.
- **Async and cancel (stream).** Run `$V tx --exit 0 --stdout '"id":"verifyasync1","status":"starting"' --stdout 'HTTP 202' serve-http-api--async 'curl -sS -X POST "$SERVE_URL/predictions" -H "Content-Type: application/json" -H "Prefer: respond-async" -d "{\"id\":\"verifyasync1\",\"input\":{\"words\":\"x y z\",\"delay\":3}}" -w "\nHTTP %{http_code}\n"'`, then immediately `$V tx --exit 0 --stdout 'HTTP 200' serve-http-api--cancel 'curl -sS -X POST "$SERVE_URL/predictions/verifyasync1/cancel" -w "\nHTTP %{http_code}\n"'`, then wait for the server to finish cancelling (about 3 s: the model is mid-sleep) with `$V tx --exit 0 --stdout 'Prediction cancelled' serve-http-api--cancel-log 'for i in $(seq 1 20); do grep -a "Prediction cancelled" "$EVIDENCE_DIR/serve.log" && exit 0; sleep 1; done; exit 1'`. The log shows `Starting prediction`, `Cancelling prediction` and `Prediction cancelled` for `prediction_id=verifyasync1`.
- **Idempotent PUT (stream, after the cancel finished).** Run `$V tx --exit 0 --stdout '"id":"verifyidem1"' --stdout '"output":\["p","q"\]' --stdout 'HTTP 200' serve-http-api--put 'curl -sS -X PUT "$SERVE_URL/predictions/verifyidem1" -H "Content-Type: application/json" -d "{\"input\":{\"words\":\"p q\"}}" -w "\nHTTP %{http_code}\n"'`.
- **Shutdown.** Run `$V tx --exit 0 --stdout 'container gone, port released' serve-http-api--shutdown 'PORT=$SERVE_PORT; CID=$SERVE_CONTAINER; "$V" serve-stop; [ -z "$(docker ps -aq --no-trunc --filter id=$CID)" ] && ! lsof -nP -iTCP:$PORT -sTCP:LISTEN >/dev/null && echo "container gone, port released"'`. `serve-stop` posts `/shutdown`, the `cog serve` process exits and the container is gone.
- **Proof.** `serve.log` (full coglet log), the per-request bundles above, and the `serve-start` READY line (port, pid, container id). For the stream group repeat `$V serve-start` in the `stream` run.

## Gotchas

- `/health-check` returns HTTP 200 with `"status":"STARTING"` while setup is running. READY, not 200, means usable; `serve-start` already waits for it.
- The model has one prediction slot. A second request while one runs gets `409 At capacity - all prediction slots busy`. After `cancel`, the slot frees only when the Python code stops (the fixture sleeps up to 3 s), so wait for the `Prediction cancelled` log line before the PUT step.
- Polling a prediction's status is not supported; async results only arrive via webhooks, so prove `serve-async` by the 202 body and the server log, not by a GET.
- SSE needs `@streaming` on the runner. Against the `echo` fixture, `Accept: text/event-stream` returns 406.
- Stop with `serve-stop` (POST `/shutdown`). A signal to the `cog serve` process makes `cog` exit but was observed to leave the container running, because the server ignores SIGTERM by design (`--await-explicit-shutdown`).
- `cog serve` publishes on 127.0.0.1 only and bind-mounts `$MODEL_DIR` at `/src`, so the model directory must live under `$HOME` on colima (the helper's default does).
- `cog serve` names its image `cog-<model dir name>` (here `$IMAGE`). It does not read a tag from you; do not rely on the name `cog-model`.
- `serve.log` is the cog process's output including coglet's colored log lines. Grep for a plain phrase (as the cancel step does) or strip the escape codes first; a pattern that spans `prediction_id=...` will not match because color codes sit between the words.
- Request ids you choose (`verifyasync1`, `verifyidem1`) must be unique per server; reusing one while it is running is exactly the idempotency case.
