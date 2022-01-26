import setuptools

with open("../README.md", "r", encoding="utf-8") as fh:
    long_description = fh.read()


# Open the pyproject.toml file and read the version from there. (Without depending on `toml`)
with open("../pyproject.toml", "r", encoding="utf-8") as fh:
    version_line = [line for line in fh if line.startswith("version")][0]
    version = version_line.split("=")[1].strip().strip('"').strip("'")


setuptools.setup(
    name="cog",
    version=version,
    author_email="team@replicate.com",
    description="Containers for machine learning",
    long_description=long_description,
    long_description_content_type="text/markdown",
    url="https://github.com/replicate/cog",
    license="Apache License 2.0",
    python_requires=">=3.6.0",
    install_requires=[
        # intionally loose. perhaps these should be vendored to not collide with user code?
        "flask>=2,<3",
        "redis>=4,<5",
        "requests>=2,<3",
        "PyYAML",
    ],
    packages=setuptools.find_packages(),
)
