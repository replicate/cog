# Cog 0.17.0-rc.2

## Prerelease CLI

```bash
# macOS ARM
sudo curl -o /usr/local/bin/cog -L \
  https://github.com/replicate/cog/releases/download/v0.17.0-rc.2/cog_Darwin_arm64
sudo chmod +x /usr/local/bin/cog
sudo xattr -d com.apple.quarantine /usr/local/bin/cog
```

## Prerelease SDK

Pin `sdk_version` in your `cog.yaml` to use the RC SDK and coglet inside the container:

```yaml
build:
  python_version: "3.14"
  python_requirements: requirements.txt
  sdk_version: "0.17.0rc2"
predict: "predict.py:Predictor"
```

Without `sdk_version`, the CLI installs whatever SDK version it was built with.
The RC coglet (Rust HTTP server) is pulled automatically as a dependency of the SDK.

## Try it

```bash
cd examples/rc
cog predict -i text=hello
# => HELLO
```

Add `--debug` to see the coglet boot logs with version info.
