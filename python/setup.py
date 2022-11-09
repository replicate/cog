import setuptools

with open("../README.md", "r", encoding="utf-8") as fh:
    long_description = fh.read()


setuptools.setup(
    name="cog",
    version="0.0.1",
    author_email="team@replicate.com",
    description="Containers for machine learning",
    long_description=long_description,
    long_description_content_type="text/markdown",
    url="https://github.com/replicate/cog",
    license="Apache License 2.0",
    python_requires=">=3.6.0",
    install_requires=[
        # intentionally loose. perhaps these should be vendored to not collide with user code?
        "attrs>=20.1,<23",
        "fastapi>=0.6,<1",
        "opentelemetry-exporter-otlp>=1.11.1,<2",
        "opentelemetry-sdk>=1.11.1,<2",
        "protobuf<=3.20.3",
        "pydantic>=1,<2",
        "PyYAML",
        "redis>=4,<5",
        "requests>=2,<3",
        "typing_extensions>=4.1.0",
        "uvicorn[standard]>=0.12,<1",
    ],
    packages=setuptools.find_packages(),
)
