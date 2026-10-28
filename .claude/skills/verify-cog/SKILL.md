---
name: verify-cog
description: Build this checkout of Cog (Go CLI, Python SDK wheel, Rust coglet wheel) and drive it the way a model author does (cog init, doctor, build, run, serve, exec, plus the coglet HTTP prediction API) against a tiny CPU model in Docker, capturing transcripts as evidence. Use when you changed cmd/, pkg/, python/ or crates/ and must prove the real CLI/SDK/server behaves, or when asked to run, smoke test or verify Cog locally. Not for unit tests, lint or GPU/registry paths.
---

# Verify Cog

Proves a change in this repo by running the freshly built `cog` binary, the SDK wheel and the coglet wheel from **this checkout** on a throwaway model, then capturing what happened. You are probably reading this mid-task with no prior knowledge of the repo, so everything you need is here. Unit tests, lint and format are separate gates (`AGENTS.md`: `mise run test:go`, `lint`, ...) and are not what this skill does.

## Surfaces

| Surface                        | What a user touches                                                                      | How this skill drives it                                                                           |
| ------------------------------ | ---------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| **`cog` CLI** (primary)        | `cog init`, `doctor`, `build`, `run`, `serve`, `exec`                                    | The binary built into `dist/go/<os>_<arch>/cog`, run through `cogverify tx`                        |
| **Python SDK** (`python/cog/`) | `BaseRunner`, `Input()`, `Path`, `@streaming` in a model's `run.py`                      | The SDK wheel from this checkout is installed into the built image; fixtures in `fixtures/` use it |
| **coglet server** (`crates/`)  | HTTP API inside the container: `/health-check`, `/predictions`, SSE, cancel, `/shutdown` | `cog serve` on a free port, then `curl`                                                            |

Not covered, say so in your report instead of implying coverage: GPU (`gpu: true`, CUDA/torch base images), `cog login`/`cog push` and any registry (forbidden here: no credentials, no pushes), `cog playground` and `cog serve --playground` (browser UI), `--separate-weights`/`weights:` flows, `build.python_requirements` with real packages, Python versions other than 3.12, Windows/WSL, native linux/amd64 hosts (this workstation emulates amd64, see Environment).

## Prelude (start every Bash call with this)

Shell state does not survive between calls, so repeat it. `<checkout>` is the absolute path of the repo you are verifying (a worktree is fine).

```bash
cd <checkout>
V=<checkout>/.claude/skills/verify-cog/scripts/cogverify   # the helper, see Helpers
export COG_VERIFY_RUN=<run id from new-run>                 # not needed for build/doctor/free-port
eval "$($V env)"      # exports COG (the built binary), IMAGE, MODEL_DIR, EVIDENCE_DIR, RUN_DIR, REPO, and SERVE_* once a server runs
```

Always call the CLI as `"$COG"`, never `cog` from `PATH`: a globally installed `cog` is a different build.

## Launch

Launch for a CLI means building the three artifacts once per change, then giving each drive its own isolated run.

```bash
$V build                     # CLI + SDK wheel + coglet wheel into <checkout>/dist (incremental: skips what is up to date)
$V build --force             # rebuild everything
$V build --skip-coglet       # CLI/SDK-only change; images then take coglet from PyPI, so crates/ is NOT under test
$V doctor                    # must end with "doctor: OK" before you drive anything
$V new-run                   # default fixture echo; --fixture files|stream for the others. Prints: export COG_VERIFY_RUN=<id>
```

Timings on this workstation: CLI ~50 s, SDK wheel ~5 s, coglet wheel ~50 s (plus a one-time ~30 s pull of the maturin image), first image build ~20 s, `serve-start` to READY ~15 s.

`mise` (what `AGENTS.md` prescribes) is **not installed** here, so `cogverify build` runs the commands the mise tasks run, with these differences. If you install mise later, `mise run build:cog build:sdk build:coglet:wheel:linux-x64` yields equivalent artifacts in `dist/`; keep driving the binary `cogverify` reports.

| mise task                         | What `cogverify build` does instead                                                                                                                                                      | Why                                                                                                                                                                                                                |
| --------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `build:cog` (goreleaser snapshot) | `CGO_ENABLED=1 go build` with the same `-X pkg/global.Version/Commit/BuildTime` ldflags. Version is `<VERSION.txt>-dev+g<sha>`, the same shape goreleaser produces.                      | Avoids goreleaser's `go mod tidy` hook, which can dirty `go.mod`/`go.sum`. The `+g<sha>` makes the CLI treat itself as a dev build, which is what turns on auto-detection of wheels in `dist/`.                    |
| `build:sdk`                       | Identical: `SETUPTOOLS_SCM_PRETEND_VERSION=<pep440 of VERSION.txt> uv build --out-dir=dist .`                                                                                            |                                                                                                                                                                                                                    |
| `build:coglet:wheel:linux-x64`    | The same `maturin build --release --target x86_64-unknown-linux-gnu --zig` inside a throwaway `ghcr.io/pyo3/maturin` container (repo mounted read-only, target dir inside the container) | Homebrew Rust here has no rustup, so the host cannot add the linux target. Cog images are **always linux/amd64** (`pkg/docker/buildkit.go`), so an x86_64 wheel is right on any host; an aarch64 wheel is ignored. |

Side effects in the checkout are limited to gitignored paths: `dist/`, `python/cog/_version.py` and `python/cog.egg-info/` (written by `uv build`, same as `mise run build:sdk`). `git status` should show no tracked-file changes after a build.

Outputs: `dist/go/<goos>_<goarch>/cog`, `dist/cog-<ver>-py3-none-any.whl`, `dist/coglet-<ver>-cp310-abi3-manylinux_2_17_x86_64.manylinux2014_x86_64.whl`. `dist/` is gitignored and shared by everything driven from this checkout, so never run two `build`s in one checkout at once, and rebuild after editing sources (doctor tells you when a wheel or binary is stale).

If the coglet build log says `self-signed certificate in certificate chain`, the network re-signs crates.io/static.rust-lang.org traffic (Cloudflare Zero Trust on this workstation). `build` already exports the macOS System-keychain CA named "Cloudflare Corporate Zero Trust" into `~/.cache/cog-verify/extra-ca.pem` and trusts it inside the builder container. Elsewhere set `COG_VERIFY_EXTRA_CA=/path/to/ca.pem` (must live under `$HOME`).

## Doctor

`$V doctor` is read-only. Run it before the first drive and whenever anything looks off. It checks: required tools, Docker daemon reachable (and its architecture), the CLI binary's version contains `+g<HEAD sha>` and is not older than `cmd/ pkg/ go.mod`, exactly one SDK wheel matching `VERSION.txt` and not older than `python/`, exactly one x86_64 coglet wheel not older than `crates/`, and, with `COG_VERIFY_RUN` set, the run's server: recorded pid alive, container running, **our** container is the only publisher of the port, `/health-check` says `READY`. Exit 1 on any FAIL.

A DIRTY tree note is informational: the version string only encodes HEAD, so uncommitted edits are covered only because `doctor` also compares file mtimes against the artifacts.

## Drive

Pick the feature file(s) from [`features/README.md`](features/README.md) that match the change, then follow them literally:

| You changed                                                               | Drive at least                                                                   |
| ------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `pkg/cli/`, `pkg/config/`, `pkg/dockerfile/`, `pkg/docker/`, `pkg/image/` | `build-image`, `run-prediction`, plus the command you touched                    |
| `python/cog/` (SDK types, runner, server)                                 | `run-prediction` (fixtures `echo`, `files`), `serve-http-api` (fixture `stream`) |
| `crates/` (coglet)                                                        | `serve-http-api` with both `echo` and `stream`; `run-prediction`                 |
| `cog doctor` checks, `cog init` templates                                 | `init-and-doctor`                                                                |
| `cog exec`                                                                | `exec-environment`                                                               |

Every command goes through the transcript helper so the evidence is automatic:

```bash
$V tx [--exit N] [--stdout RE] [--stderr RE] [--stdout-not RE] [--stderr-not RE] NAME 'COMMAND'
```

`COMMAND` runs under `bash -c` with cwd `$MODEL_DIR` and stdin `/dev/null`, with `$COG $IMAGE $MODEL_DIR $EVIDENCE_DIR $REPO $SERVE_URL $SERVE_PORT $SERVE_CONTAINER $V` exported. It prints the command, head/tail of stdout and stderr, the exit code and one `ASSERT ok|FAIL` line per assertion; the helper itself exits non-zero if an assertion failed. Cog writes progress and most human output to **stderr** (`init` and `doctor` write everything there); results such as a prediction's output go to stdout. Name steps `<feature-id>--<step>`.

Servers: `$V serve-start` picks a free port (never 8393, 5000 or 7000), runs `"$COG" serve -p <port>` in the background, waits for `/health-check` = READY and records pid, port and container id; `$V serve-stop` stops it via `POST /shutdown`. Do not stop it with a signal: the coglet server runs with `--await-explicit-shutdown` (ignores SIGTERM), and a SIGINT to the `cog` process was observed to exit `cog` while leaving the container running.

Fixtures (copied into a fresh `$MODEL_DIR` by `new-run`, `python_version: "3.12"`, no pip requirements, CPU only):

| Fixture          | Runner                                                                          | Use for                             |
| ---------------- | ------------------------------------------------------------------------------- | ----------------------------------- |
| `echo` (default) | `run(text: str, times: int = 2 [1..5]) -> str` returns `verify:<text>` repeated | build, run, serve, validation, exec |
| `files`          | `run(doc: Path) -> Path` upper-cases a text file                                | file input/output                   |
| `stream`         | `@streaming run(words: str, delay: float = 0.0) -> Iterator[str]`               | SSE, async, cancel, idempotent PUT  |

To verify a change that needs a different model shape, edit the copy in `$MODEL_DIR` (never the fixture in the repo) and say so in the report.

## Evidence

Evidence lives **outside the repo**, in `$EVIDENCE_DIR` = `~/.cache/cog-verify/runs/<run id>/evidence/` (override the root with `COG_VERIFY_HOME`, which must stay under `$HOME` because colima only shares `$HOME` with containers, not `/tmp`). `tx` writes `NAME.cmd`, `.stdout`, `.stderr`, `.exit` and `.asserts` there; `serve-start` writes `serve.log` (coglet's structured log, the place to see cancel and shutdown events); `cleanup` adds `run-state.env`. Report the directory path.

Proof standards for Cog:

- Exercise the real path: the built binary against a real Docker image. Do not call SDK or coglet internals, and do not import `cog` on the host (host Python is 3.9 and unrelated to the image).
- Prove the binary under test built the image: `docker image inspect "$IMAGE"` label `run.cog.version` equals `"$COG" --version`'s version, and `run.cog.pip_freeze` shows `cog @ file:///tmp/cog-...whl` and `coglet @ file:///tmp/coglet-...whl` (local wheels), not PyPI pins. `cog build --debug` prints `Using local cog wheel:` and `Using local coglet wheel:`. `build-image` has the recipe.
- Capture the action and the resulting state: the command and its exit code, then a second view (image labels, `docker ps`, the HTTP response, the file the container wrote to the mounted dir).
- Verify side effects: images created (`docker images`), containers running or gone, ports published and released, files written into `$MODEL_DIR`.
- Negative paths count: out-of-range and missing inputs must fail with the documented message and exit code, ideally before a container starts.
- `cog run` without an image argument and `cog exec` rebuild the image from `cog.yaml` (cached layers make it fast); neither has a dry-run. Nothing here pushes to a registry; `cog push` and `cog login` must never run.

## Cleanup

```bash
$V cleanup                  # stop the run's server, remove images named <IMAGE>*, delete $MODEL_DIR; KEEPS $EVIDENCE_DIR
$V cleanup --purge-tools    # additionally remove the cached maturin image and extra-ca.pem
```

Cleanup only touches things this run created and recorded: the `cog serve` pid and container id it started, images tagged `cog-verify-<id>` (the model directory is named `verify-<id>`, and cog names images `cog-<dir>`), and the run's directory. It never kills by process name and never prunes Docker. It leaves `dist/` (gitignored build outputs reused by the next run) and the buildkit/base-image cache (`r8.im/cog-base`, `ghcr.io/astral-sh/uv`) alone because other work on the machine shares them. After a failed attempt run `cleanup` before retrying so ports and images are not stranded; use `git status` to confirm only `dist/`-style ignored files changed.

Two Docker rules for a shared daemon: do not `docker run`/`pull --platform linux/amd64` a tag that already exists locally as arm64 (it re-points the tag), and never remove an image or container you did not create.

## Isolation

Safe to run several drives side by side: each run has its own run id, free port, model directory and image tag. Not isolated: `dist/` (one per checkout), the Docker daemon and its build cache, and the fixed names `cog serve` uses inside the container (internal port 5000 only; the host port is yours).

## Environment facts (this workstation, as verified)

- macOS arm64, Go from Homebrew, `uv` and Docker CLI present, **no `mise`, no `rustup`, no `zig`**.
- Docker is colima (vz, virtiofs) running an aarch64 Ubuntu VM. Cog forces `linux/amd64`, so everything in the image runs emulated (slower; `<jemalloc>: MADV_DONTNEED does not work` lines in logs are the emulation talking, not a bug). Only `$HOME` is shared into the VM: a model directory under `/tmp` cannot be bind-mounted by `cog serve`/`cog exec`.
- `cog build` always prints `#4 ERROR: rpc error: code = NotFound ... no access allowed to dir "context"` in plain progress output while still exiting 0 and producing the image; it is a BuildKit quirk, not a failure.
- coglet built in the container reports `git_sha: "unknown"` (no `.git` inside); identify it by `build_time` and the wheel's mtime instead.
- The network re-signs some TLS (crates.io, static.rust-lang.org) but not pypi.org, ghcr.io or r8.im; the fixtures need no pip packages so image builds stay off the intercepted domains.

## Helpers

`scripts/cogverify` (bash 3.2 compatible, executable). Invocation: `$V <command>`.

| Command                                   | Purpose                                                                                          |
| ----------------------------------------- | ------------------------------------------------------------------------------------------------ |
| `build [--force] [--skip-coglet]`         | Build CLI, SDK wheel and x86_64 coglet wheel into `<checkout>/dist`, skipping what is current    |
| `doctor`                                  | Read-only readiness check, exit 1 on FAIL                                                        |
| `new-run [--fixture echo\|files\|stream]` | Create run dirs and a scratch model; prints `export COG_VERIFY_RUN=<id>`                         |
| `env`                                     | Print the run's variables for `eval`                                                             |
| `tx [asserts] NAME 'COMMAND'`             | Run a command in `$MODEL_DIR` and save its transcript as evidence                                |
| `serve-start` / `serve-stop`              | Start `cog serve` on a free port and wait for READY / stop it cleanly                            |
| `cleanup [--purge-tools]`                 | Tear down what the run created, keep the evidence                                                |
| `image-label KEY`                         | Print one label of the run's image (for example `run.cog.version`, `run.cog.pip_freeze`)         |
| `image-schema`                            | Summarize the OpenAPI Input schema stored in the run's image (type, required, default, min, max) |
| `free-port`                               | Print a free localhost port                                                                      |

Environment knobs: `COG_VERIFY_HOME` (default `~/.cache/cog-verify`), `COG_VERIFY_EXTRA_CA`, `COG_VERIFY_MATURIN_IMAGE`, `COG_VERIFY_SERVE_TIMEOUT` (default 420 s), `COG_VERIFY_TX_LINES` (transcript lines shown, default 40; files always hold everything).

## Feature map

[`features/README.md`](features/README.md) indexes one recipe per user-facing feature. Keep it honest with `/maintain-verification-skill` when commands or behavior change.
