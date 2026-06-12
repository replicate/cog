import tempfile

from PIL import Image, ImageFilter

from cog import BaseRunner, Input, Path


class Runner(BaseRunner):
    def run(
        self,
        image: Path = Input(description="Input image"),
        blur: float = Input(description="Blur radius", default=5),
    ) -> Path:
        if blur == 0:
            return input
        im = Image.open(str(image))
        im = im.filter(ImageFilter.BoxBlur(blur))
        out_path = Path(tempfile.mkdtemp()) / "out.png"
        im.save(str(out_path))
        return out_path
