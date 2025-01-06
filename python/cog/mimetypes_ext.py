import sys
import typing

if typing.TYPE_CHECKING:
    from typing_extensions import Protocol
else:
    Protocol = object


class IMimeTypes(Protocol):
    def add_type(self, type: str, ext: str, strict: bool = True) -> None: ...


def install_mime_extensions(mimetypes: IMimeTypes) -> None:
    """
    Older versions of Python are missing the MIME types for more recent file formats
    this function adds the missing MIME types to the mimetypes module.
    """

    # This could also be done by loading a mime.types file from disk using
    # mimetypes.read_mime_types().
    if sys.version_info < (3, 13):
        mimetypes.add_type("image/webp", ".webp")
