from cog import Input, Path


def predict(
    img: Path = Input(
        description='Reference image of the character whose face to swap'
    ),
) -> Path:
    print('img', type(img), img)
    return img
