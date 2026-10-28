# Run a command in the model environment

`cog exec <command>` builds an image from `cog.yaml` (cached), starts a container with the current directory mounted at `/src`, runs the command in it and returns when it exits. It is how authors debug the exact environment their model will run in.

## Sub-features

- `exec-env` runs the command inside the configured Python with the SDK and coglet from this checkout installed.
- `exec-mount` shares the project directory: a file the container writes appears on the host.
- `exec-exit` makes `cog exec` exit 1 and print `container exited with status N` when the command fails.
- `exec-stdin` forwards piped stdin to the command.

## How to get to it (user POV)

- Run `cog exec python` (interactive shell; not covered, needs a TTY).
- Run `cog exec python script.py` or any other command in the project directory.
- Run `echo data | cog exec cat` with piped stdin.

## Driving it with cogverify

Preconditions:

- `doctor: OK`; a run with fixture `echo`. No prior build is needed; `exec` builds `$IMAGE` itself.
- Use `--progress quiet` so transcripts show the command's own output.

- **Environment.** Run `$V tx --exit 0 --stdout '^3\.12\.' --stdout "^cog $(tr -d '[:space:]' < "$REPO/VERSION.txt") coglet $(tr -d '[:space:]' < "$REPO/VERSION.txt")$" exec-environment--env '"$COG" exec --progress quiet python -c "import sys, importlib.metadata as m; print(sys.version.split()[0]); print(\"cog\", m.version(\"cog\"), \"coglet\", m.version(\"coglet\"))"'`. Python is 3.12.x and both packages report the checkout's `VERSION.txt` version.
- **Mounted directory.** Run `$V tx --exit 0 --stdout '^from-container$' exec-environment--mount 'rm -f made-in-container.txt; "$COG" exec --progress quiet sh -c "echo from-container > made-in-container.txt"; cat made-in-container.txt'`. The file exists in `$MODEL_DIR` on the host.
- **Failing command.** Run `$V tx --exit 1 --stderr 'container exited with status 3' exec-environment--exit '"$COG" exec --progress quiet sh -c "exit 3"'`.
- **Stdin.** Run `$V tx --exit 0 --stdout '^hello-stdin$' exec-environment--stdin 'echo hello-stdin | "$COG" exec --progress quiet cat'`.
- **Proof.** The four bundles, plus `ls "$MODEL_DIR"` after the mount step showing `made-in-container.txt` next to `cog.yaml` and `run.py`.

## Gotchas

- `cog exec` does not pass the command's exact exit status through: any non-zero status becomes exit 1 with the real value only in the message. Assert on the message.
- `exec` rebuilds the image on every call (layers are cached); `cog.yaml` problems surface as build errors before the command starts.
- The first line printed by a plain (non-quiet) run is the whole BuildKit log on stderr; assert on stdout for the command's output.
- Interactive use (`cog exec python` with a TTY) cannot be driven through `tx` (stdin is `/dev/null`); say so rather than claiming coverage.
- The image is named `cog-<model dir>` (`$IMAGE`); cleanup removes it. A host file written through the mount is owned by the host user on colima/virtiofs.
