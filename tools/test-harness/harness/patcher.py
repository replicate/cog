"""Patch cog.yaml files with sdk_version and arbitrary overrides."""

from __future__ import annotations

import copy
from pathlib import Path
from typing import Any

import yaml


def deep_merge(base: dict[str, Any], override: dict[str, Any]) -> dict[str, Any]:
    """Recursively merge *override* into *base*, returning a new dict."""
    result = copy.deepcopy(base)
    for key, value in override.items():
        if key in result and isinstance(result[key], dict) and isinstance(value, dict):
            result[key] = deep_merge(result[key], value)
        else:
            result[key] = copy.deepcopy(value)
    return result


def patch_cog_yaml(
    cog_yaml_path: Path,
    sdk_version: str | None = None,
    overrides: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """Read a cog.yaml, apply patches, write it back, and return the final config.

    Parameters
    ----------
    cog_yaml_path:
        Path to the cog.yaml file to patch (modified in-place).
    sdk_version:
        If set, inject ``build.sdk_version`` into the config.
    overrides:
        Arbitrary dict that is deep-merged into the config.  Useful for
        changing python_version, adding system_packages, etc.

    Returns
    -------
    The patched config dict.
    """
    with open(cog_yaml_path) as f:
        config = yaml.safe_load(f) or {}

    if sdk_version:
        config.setdefault("build", {})
        config["build"]["sdk_version"] = sdk_version

    if overrides:
        config = deep_merge(config, overrides)

    with open(cog_yaml_path, "w") as f:
        yaml.dump(config, f, default_flow_style=False, sort_keys=False)

    return config
