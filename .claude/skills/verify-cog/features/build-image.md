# Build an image

`cog build -t <name>` turns the project in the current directory into a linux/amd64 Docker image that contains the model code, the Cog SDK and coglet, and labels describing the config and the generated OpenAPI schema.

## Sub-features

- `build-tag` builds and tags an image and exits 0 with `Image built as <tag>`.
- `build-local-wheels` installs the SDK and coglet wheels from this checkout's `dist/`, not from PyPI.
- `build-labels` records the CLI version, config and pip freeze in `run.cog.*` image labels.
- `build-schema` embeds an OpenAPI schema that reflects the runner's inputs, defaults, constraints and descriptions.
- `build-bad-config` fails fast with a non-zero exit and a precise message for an invalid or inconsistent `cog.yaml`.

## How to get to it (user POV)

- Run `cog build -t <name>` in the project directory.
- Run `cog build` with no tag (cog names the image `cog-<directory>`).
- Run `cog build --debug` to see which wheels are used.
- Run `cog run`, `cog serve` or `cog exec` without an image argument; each builds the same image first (covered in their features).

## Driving it with cogverify

Preconditions:

- `doctor: OK`; a run with the `echo` fixture (`$V new-run`). `$IMAGE` is `cog-verify-<id>`, unused so far.
- `dist/` holds the current CLI, SDK wheel and x86_64 coglet wheel.

- **Build and tag.** Run `$V tx --exit 0 --stderr 'Image built as '"$IMAGE" --stderr 'Using local coglet wheel:' --stderr 'Using local cog wheel:' build-image--build-tag '"$COG" build -t "$IMAGE" --debug --progress plain'`. Exit 0, the debug lines name the wheels under `<checkout>/dist/`. (The `#4 ERROR ... no access allowed to dir "context"` line is a known BuildKit quirk; the exit code decides.)
- **Platform.** Run `$V tx --exit 0 --stdout '^linux/amd64$' build-image--platform 'docker image inspect "$IMAGE" --format "{{.Os}}/{{.Architecture}}"'`.
- **Provenance.** Run `$V tx --exit 0 --stdout "run.cog.version: [0-9.]+-dev\+g$(git -C "$REPO" rev-parse --short HEAD)\$" --stdout 'cog @ file:///tmp/cog-' --stdout 'coglet @ file:///tmp/coglet-' --stdout-not 'coglet==' build-image--build-labels '"$V" image-label run.cog.version | sed "s/^/run.cog.version: /"; "$V" image-label run.cog.pip_freeze; "$COG" --version'`. The label equals the version `"$COG" --version` prints on the last line, and the pip freeze pins the SDK and coglet to `file:///tmp/...whl` copies, with no `coglet==` PyPI pin.
- **Schema.** Run `$V tx --exit 0 --stdout '^text: type=string required=yes default=None minimum=None maximum=None' --stdout '^times: type=integer required=no default=2 minimum=1 maximum=5 description=.Repeat count.' build-image--build-schema '"$V" image-schema'`. The summary is read from the image's `run.cog.openapi_schema` label: `text` is required, `times` has default 2 and bounds 1..5.
- **Config errors.** Run `$V tx --exit 1 --stderr 'invalid build.python_version "2.7"' build-image--bad-python 'sed -i.bak "s#\"3.12\"#\"2.7\"#" cog.yaml && "$COG" build -t "$IMAGE-bad" --progress quiet; rc=$?; mv cog.yaml.bak cog.yaml; exit $rc'`, then `$V tx --exit 1 --stderr 'missing.py' --stderr 'no such file' build-image--missing-runner 'sed -i.bak "s#run.py:Runner#missing.py:Runner#" cog.yaml && "$COG" build -t "$IMAGE-bad" --progress quiet; rc=$?; mv cog.yaml.bak cog.yaml; exit $rc'`, then `$V tx --exit 0 --stdout '^0$' build-image--no-bad-image 'docker images -q --filter "reference=$IMAGE-bad*" | wc -l | tr -d " "'`. Both builds fail before any image exists, and `cog.yaml` is restored each time.
- **Proof.** `build-image--build-tag.stderr` (wheel lines and `Image built as`), `build-image--build-labels.stdout` (version and wheel pins next to the CLI's own version), and a closing `$V tx --exit 0 build-image--images 'docker images --filter "reference=$IMAGE*"'` showing the one image and nothing else.

## Gotchas

- Cog images are always linux/amd64 even on arm64 hosts (the helper's coglet wheel is x86_64 for that reason). If `build-local-wheels` shows `coglet==...` from PyPI instead of a `file:///tmp/` pin, the dist wheel was missing or for the wrong platform; rerun `$V build` and `$V doctor`.
- `run.cog.version` ends in `+g<sha>` of HEAD at build time. Uncommitted edits are not in that string; doctor's mtime checks are what tell you the binary is stale.
- The plain progress log contains `#4 ERROR: ... no access allowed to dir "context"` on every build. It is benign; do not count it as a failure and do not hide a real failure behind it.
- `--debug` also prints `$ docker-credential-osxkeychain get`; that is cog reading local Docker credentials for base-image pulls. It does not use Replicate credentials and nothing is pushed.
- Using `python_requirements` with real packages reaches PyPI and mirrors from inside the VM; on this network some domains are TLS-intercepted. The fixtures avoid that on purpose.
- `cog build` has no dry-run and always writes an image; `cleanup` removes `$IMAGE*` only.
