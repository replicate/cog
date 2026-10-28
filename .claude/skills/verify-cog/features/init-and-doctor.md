# Initialize and diagnose a project

A model author scaffolds a project with `cog init` and checks it with `cog doctor`, which reports config and code problems, exits non-zero on errors, and can rewrite files with `--fix`.

## Sub-features

- `init-scaffold` creates `cog.yaml`, `run.py`, `requirements.txt`, `.dockerignore` and `workflows/push.yaml` in the current directory.
- `doctor-clean` reports a fresh project with no errors and exit code 0.
- `doctor-error` reports a missing runner file as an error and exits 1.
- `doctor-fix` rewrites a deprecated import in place when given `--fix` and reports what it fixed.

## How to get to it (user POV)

- Run `cog init` in an empty directory.
- Run `cog doctor` in a project directory.
- Run `cog doctor --fix` in a project directory.
- Run `cog doctor -f <file>` to point at a config file other than `cog.yaml` (not covered by the steps below).

## Driving it with cogverify

Preconditions:

- `doctor: OK` from `$V doctor`; a run exists (any fixture; no image is built here).
- All steps work in a subdirectory `initcheck/` of `$MODEL_DIR`, which `cleanup` deletes. `cog init` must run in a directory without an existing `cog.yaml`.

- **Scaffold.** Run `$V tx --exit 0 --stderr 'Creating cog.yaml' --stderr 'Creating run.py' --stdout 'run: "run.py:Runner"' init-and-doctor--init-scaffold 'mkdir -p initcheck && cd initcheck && "$COG" init && ls -A && grep -E "^run:|python_version" cog.yaml && grep -n "class Runner" run.py'`. The listing shows `.dockerignore cog.yaml requirements.txt run.py workflows`, `cog.yaml` has `run: "run.py:Runner"`, and `run.py` defines `class Runner(BaseRunner)`.
- **Clean diagnosis.** Run `$V tx --exit 0 --stderr 'Config parsing' --stderr 'Predict reference' --stderr-not 'Found [0-9]+ error' init-and-doctor--doctor-clean 'cd initcheck && "$COG" doctor'`. Every check name is listed; the summary is `Found N warning` or no summary, never an error. A `Python version` warning about the host Python is normal.
- **Error state.** Break the runner reference and run doctor. Run `$V tx --exit 1 --stderr 'missing.py not found' --stderr 'doctor found issues' --stderr 'Found 1 error' init-and-doctor--doctor-error 'cd initcheck && sed -i.bak "s#run.py:Runner#missing.py:Runner#" cog.yaml && "$COG" doctor; rc=$?; mv cog.yaml.bak cog.yaml; exit $rc'`. The Predict reference check names `missing.py`, and `cog.yaml` is restored afterwards.
- **Fix state.** Add an import that cog removed and let doctor fix it. Run `$V tx --exit 0 --stderr 'Fixed: Deprecated imports' --stderr 'ExperimentalFeatureWarning was removed' --stdout-not 'ExperimentalFeatureWarning' init-and-doctor--doctor-fix 'cd initcheck && printf "from cog.types import ExperimentalFeatureWarning\n" | cat - run.py > run.py.new && mv run.py.new run.py && "$COG" doctor --fix; rc=$?; head -n 3 run.py; exit $rc'`. The summary says `Fixed 1 issue`, and the first lines of `run.py` no longer contain the import.
- **Proof.** The five-file bundle for each step under `$EVIDENCE_DIR`, plus `ls -A initcheck` in the scaffold step's stdout.

## Gotchas

- `init` and `doctor` write all their output to stderr. Asserting on stdout silently fails (the only stdout is whatever your own command prints, like the `ls`).
- Warnings do not change the exit code; only errors do (exit 1). The `Python version` warning depends on the host's `python3` and is not a regression.
- `doctor --fix` edits files in place. Only run it inside `initcheck/` or another scratch copy.
- `cog init` creates `workflows/push.yaml`, a GitHub Actions template. It does not push or contact anything.
- The scaffolded `run.py` has an empty body and returns nothing; do not build and run it as a working model (use a fixture for that).
