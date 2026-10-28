# Cog verification map

This directory is the maintained source for verifying the user-facing behavior of Cog (CLI, Python SDK, coglet server). Read the index before driving, then use the matching feature file as the recipe. `../SKILL.md` explains the helper (`cogverify`), the prelude every Bash call starts with, and cleanup.

## Baseline preconditions

- `$V build` has run for the current sources, and `$V doctor` ends with `doctor: OK` (CLI version contains `+g<HEAD sha>`, SDK wheel and x86_64 coglet wheel present and fresh).
- A run exists: `$V new-run` (or `--fixture files|stream`), `COG_VERIFY_RUN` exported, `eval "$($V env)"` done. One run per drive; never share a run between concurrent agents.
- Docker answers (`docker info`) and can reach `r8.im`, `ghcr.io` and `docker.io` for base images (cached after the first build).
- Call the CLI only as `"$COG"`. Never run `cog login` or `cog push`, never use Replicate credentials, never publish to a registry.
- Ports come only from `$V serve-start` (it picks a free one). Never bind 8393 (cog's default), 5000, 7000 or 9222.
- Never drive a container, image or port that this run did not create.

## Driving conventions

- Start every recipe from the baseline state unless its preconditions say otherwise. Recipes that mutate `$MODEL_DIR` restore it in the same command.
- Run every step through `$V tx [--exit N] [--stdout RE] [--stderr RE] NAME 'COMMAND'`. Quote the command in single quotes so `$COG`, `$IMAGE`, `$SERVE_URL` expand inside the helper, not in your shell.
- Treat commands as literal. Keep flags, quoted values and input names unchanged.
- Name each step `<feature-id>--<step>` so the evidence folder reads as an index. The recipes below already do.
- Cog prints progress and diagnostics to stderr (`init` and `doctor` print everything there). Assert prediction results on stdout and messages on stderr.
- Prefer `--progress quiet` when build noise is not what you are checking; use `--progress plain` or `--debug` when it is.
- Wait for readiness, not for time: `serve-start` returns only when `/health-check` says `READY`. After an async prediction or cancel, wait for the server log line, not a fixed sleep longer than the recipe says.
- Finish with `$V cleanup` (and `serve-stop` first if you need the server's end state as evidence).

## Proof and skip reporting

- Capture the user action and the resulting state, not only the final message: the command and exit code, then a second read-only view (image labels, `docker ps`, the HTTP body, `serve.log`, the file a container wrote).
- CLI proof is the `tx` bundle: command, stdout, stderr, exit code and assertion lines under `$EVIDENCE_DIR`.
- Server proof adds the HTTP status and body for each request and the relevant `serve.log` lines.
- Image proof includes `run.cog.version` equal to the CLI's version and local-wheel pins in `run.cog.pip_freeze` (see `build-image`).
- Record the feature id and entry point used with every artifact (the step name carries it).
- Report an unreachable path with the attempted command and the unmet precondition. Do not report a skipped entry point as verified through a different path (for example `cog predict` is a deprecated alias of `cog run`; verifying one does not verify the other).
- Out of scope for this map (say so rather than implying coverage): GPU and CUDA base images, `cog login`, `cog push`, registries, `cog playground` and `--playground`, weights flows, real `python_requirements`, Python versions other than 3.12.

## Feature entry contract

Each feature file starts with an H1 title and one paragraph describing the user-visible behavior. It then uses exactly four H2 sections in this order.

1. `Sub-features` lists short IDs with one line for each behavior.
2. `How to get to it (user POV)` lists every user entry point.
3. `Driving it with cogverify` starts with `Preconditions:` and uses labeled bullets that pair each user action with an exact command and observable result.
4. `Gotchas` lists traps that can waste or invalidate a verification run.

Keep implementation details out of the map. Name only user paths, stable handles, required state, commands, and observable proof.

## Features

- [Initialize and diagnose a project](./init-and-doctor.md) covers `cog init` scaffolding and `cog doctor` clean, error and `--fix` states.
- [Build an image](./build-image.md) covers `cog build -t`, local-wheel provenance, image labels, the generated OpenAPI schema and config errors.
- [Run a prediction from the CLI](./run-prediction.md) covers `cog run <image>` with defaults, validation errors, JSON input, file input/output and the deprecated `cog predict` alias.
- [Serve the HTTP prediction API](./serve-http-api.md) covers `cog serve`, the coglet endpoints (health, predictions, validation, OpenAPI, SSE, async, cancel, idempotent PUT) and clean shutdown.
- [Run a command in the model environment](./exec-environment.md) covers `cog exec` environment, mounted directory, exit status and stdin.
