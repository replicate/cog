import logging
import os
from pathlib import Path
from typing import Union

log = logging.getLogger(__name__)

PathLike = Union[Path, str, None]


class ProbeHelper:
    _root = Path("/var/run/cog")
    _enabled = False

    def __init__(self, root: PathLike = None) -> None:
        if "KUBERNETES_SERVICE_HOST" not in os.environ:
            log.info("Not running in Kubernetes: disabling probe helpers.")
            return

        if root is not None:
            self._root = Path(root)

        try:
            self._root.mkdir(exist_ok=True, parents=True)
        except OSError:
            log.error(
                f"Failed to create cog runtime state directory ({self._root}). "
                "Does it already exist and is a file? Does the user running cog "
                "have permissions?"
            )
        else:
            self._enabled = True

    def ready(self) -> None:
        if self._enabled:
            (self._root / "ready").touch()
