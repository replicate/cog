from mimetypes import MimeTypes
from cog.mimetypes_ext import install_mime_extensions


def test_webp_ext_support():
    # Assert on empty database.
    mt = MimeTypes(filenames=tuple())
    assert mt.guess_type("image.webp") == (None, None)
    install_mime_extensions(mt)
    assert mt.guess_type("image.webp") == ("image/webp", None)

    # Assert global override
    import cog

    import mimetypes

    assert mimetypes.guess_type("image.webp") == ("image/webp", None)
