# Environment variables

This guide lists the environment variables that change how Cog functions.

### `COG_NO_UPDATE_CHECK`

By default, Cog automatically checks for updates 
and notifies you if there is a new version available.

To disable this behavior, 
set the `COG_NO_UPDATE_CHECK` environment variable to any value.

```console
$ COG_NO_UPDATE_CHECK=1 cog build  # runs without automatic update check
```
