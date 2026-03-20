# How to set up CI/CD

This guide shows you how to build and push Cog images automatically in continuous integration pipelines.

## Install Cog in CI

Download the Cog binary in your CI job. This example uses GitHub Actions, but the approach works in any CI system:

```yaml
- name: Install Cog
  run: |
    curl -o /usr/local/bin/cog -L "https://github.com/replicate/cog/releases/latest/download/cog_$(uname -s)_$(uname -m)"
    chmod +x /usr/local/bin/cog
```

## Build and push with GitHub Actions

A complete workflow that builds a Cog image and pushes it to Replicate:

```yaml
name: Build and push model
on:
  push:
    branches: [main]

jobs:
  build-and-push:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install Cog
        run: |
          curl -o /usr/local/bin/cog -L "https://github.com/replicate/cog/releases/latest/download/cog_$(uname -s)_$(uname -m)"
          chmod +x /usr/local/bin/cog

      - name: Push to Replicate
        env:
          REPLICATE_API_TOKEN: ${{ secrets.REPLICATE_API_TOKEN }}
        run: |
          cog login --token-stdin <<< "$REPLICATE_API_TOKEN"
          cog push r8.im/your-username/my-model
```

To push to a different registry, replace the login and push steps:

```yaml
      - name: Log in to registry
        run: echo "${{ secrets.REGISTRY_PASSWORD }}" | docker login registry.example.com -u ${{ secrets.REGISTRY_USERNAME }} --password-stdin

      - name: Build and push
        run: |
          cog build -t registry.example.com/your-org/my-model:${{ github.sha }}
          docker push registry.example.com/your-org/my-model:${{ github.sha }}
```

## Test the built image

To verify the image works before pushing, build it locally and run a test prediction:

```yaml
      - name: Build image
        run: cog build -t my-model:test

      - name: Test prediction
        run: |
          docker run -d --name test-model -p 5001:5000 my-model:test
          
          # Wait for model to be ready
          for i in $(seq 1 60); do
            status=$(curl -sf http://localhost:5001/health-check | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])" 2>/dev/null)
            if [ "$status" = "READY" ]; then break; fi
            sleep 5
          done
          
          # Run a prediction
          curl -sf http://localhost:5001/predictions \
            -X POST \
            -H "Content-Type: application/json" \
            -d '{"input": {"prompt": "test"}}' | python3 -c "import sys,json; d=json.load(sys.stdin); assert d['status']=='succeeded', f'Prediction failed: {d}'"
          
          docker stop test-model
```

If your model requires a GPU, you will need a CI runner with GPU access (e.g. a self-hosted runner with NVIDIA drivers), or skip the test prediction step in CI and rely on a staging environment instead.

## Cache Docker layers for faster builds

Cog uses Docker BuildKit under the hood. To cache layers between CI runs, use Docker's built-in layer caching:

```yaml
      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Build with cache
        run: cog build -t my-model:latest
```

If builds are still slow because a dependency changed, you can force a clean build:

```console
cog build --no-cache -t my-model:latest
```

## Build only on relevant changes

To avoid unnecessary builds, use path filters to trigger the workflow only when model code changes:

```yaml
on:
  push:
    branches: [main]
    paths:
      - "predict.py"
      - "cog.yaml"
      - "requirements.txt"
      - "weights/**"
```

If your weights are stored outside the repository and downloaded in `setup()`, you can exclude the `weights/**` path.

## Use separate-weights for faster pushes

If your model includes weights in the image, use `--separate-weights` so that code-only changes do not re-upload the weights layer:

```yaml
      - name: Push to Replicate
        run: cog push r8.im/your-username/my-model --separate-weights
```

This significantly reduces push time when only your prediction code changed.

## Pin the Cog version

To ensure reproducible builds, pin the Cog version in your CI workflow:

```yaml
      - name: Install Cog
        run: |
          COG_VERSION=0.14.0
          curl -o /usr/local/bin/cog -L "https://github.com/replicate/cog/releases/download/v${COG_VERSION}/cog_$(uname -s)_$(uname -m)"
          chmod +x /usr/local/bin/cog
```

## Pin the SDK version

To ensure the same Python SDK version is installed in every build, set `sdk_version` in your `cog.yaml`:

```yaml
build:
  python_version: "3.12"
  sdk_version: "0.18.0"
```

This prevents unexpected behaviour from SDK upgrades between builds. See the [`cog.yaml` reference](../yaml.md#sdk_version) for details.

## Next steps

- See [How to deploy to production](deploy.md) for running your built image in production.
- See the [CLI reference](../cli.md) for all `cog build` and `cog push` options.
- See [How to debug build failures](debug-builds.md) if your CI builds are failing.
