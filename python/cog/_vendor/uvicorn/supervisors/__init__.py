from typing import TYPE_CHECKING, Type

from cog._vendor.uvicorn.supervisors.basereload import BaseReload
from cog._vendor.uvicorn.supervisors.multiprocess import Multiprocess

if TYPE_CHECKING:
    ChangeReload: Type[BaseReload]
else:
    try:
        from cog._vendor.uvicorn.supervisors.watchfilesreload import (
            WatchFilesReload as ChangeReload,
        )
    except ImportError:  # pragma: no cover
        try:
            from cog._vendor.uvicorn.supervisors.watchgodreload import (
                WatchGodReload as ChangeReload,
            )
        except ImportError:
            from cog._vendor.uvicorn.supervisors.statreload import StatReload as ChangeReload

__all__ = ["Multiprocess", "ChangeReload"]
