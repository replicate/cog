import setuptools

with open("../README.md", "r", encoding="utf-8") as fh:
    long_description = fh.read()


setuptools.setup(
    name="cog",
    version="0.0.1",
    author_email="team@replicate.ai",
    description="Containers for machine learning",
    long_description=long_description,
    long_description_content_type="text/markdown",
    url="https://github.com/replicate/cog",
    license="Apache License 2.0",
    python_requires=">=3.6.0",
    install_requires=[
        # intionally loose. perhaps these should be vendored to not collide with user code?
        "flask>=2,<3",
        "redis>=3,<4",
        "requests>=2,<3",
    ],
    packages=setuptools.find_packages(),
)
