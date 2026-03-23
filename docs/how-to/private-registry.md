# How to use private package registries

This guide shows you how to build a Cog Docker image that fetches Python packages from a private registry, without baking credentials into the image.

## Create a pip.conf file

In a directory **outside** your Cog project, create a `pip.conf` file with your registry credentials:

```conf
[global]
index-url = https://username:password@my-private-registry.com
```

> **Warning**
> Do not commit secrets to Git or include them in Docker images. If your Cog project contains sensitive files, add them to `.gitignore` and `.dockerignore`.

## Configure cog.yaml

Add a `run` command that mounts the pip configuration as a secret:

```yaml
build:
  run:
    - command: pip install
      mounts:
        - type: secret
          id: pip
          target: /etc/pip.conf
```

The secret mount makes the credentials available during build without persisting them in the final image. For full details on the `run` and `mounts` syntax, see the [`cog.yaml` reference](../yaml.md).

## Build with the secret

Pass the `--secret` option when building or pushing, with an `id` matching the one in `cog.yaml`:

```console
cog build --secret id=pip,source=/path/to/pip.conf
```

## Cache behaviour

If you change the contents of a secret source file after building, subsequent builds will use the cached version and ignore your changes.

To pick up updated secrets, either:

- Change the `id` value in both `cog.yaml` and the `--secret` option, or
- Pass `--no-cache` to bypass the build cache entirely
