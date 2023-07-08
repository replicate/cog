# Private package registry

This guide describes how to build a Docker image with Cog that fetches Python packages from a private registry during setup.

## `pip.conf`

In a directory outside your Cog project, create a `pip.conf` file with an `index-url` set to the registry's URL with embedded credentials.

```conf
[global]
index-url = https://username:password@my-private-registry.com
```

> **Warning**
> Be careful not to commit secrets in Git or include them in Docker images. If your Cog project contains any sensitive files, make sure they're listed in `.gitignore` and `.dockerignore`.

## `cog.yaml`

In your project's [`cog.yaml`](yaml.md) file, add a setup command to run `pip install` with a secret configuration file mounted to `/etc/pip.conf`.

```yaml
build:
  run:
    - command: pip install
      mounts:
        - type: secret
          id: pip
          target: /etc/pip.conf
```

## Build

When building or pushing your model with Cog, pass the `--secret` option with an `id` matching the one specified in `cog.yaml`, along with a path to your local `pip.conf` file.

```console
$ cog build --secret id=pip,source=/path/to/pip.conf
```

Using a secret mount allows the private registry credentials to be securely passed to the `pip install` setup command, without baking them into the Docker image.

> **Warning**
> If you run `cog build` or `cog push` and then change the contents of a secret source file, the cached version of the file will be used on subsequent builds, ignoring any changes you made. To update the contents of the target secret file, either change the `id` value in `cog.yaml` and the `--secret` option, or pass the `--no-cache` option to bypass the cache entirely.
