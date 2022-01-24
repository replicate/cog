# syntax = docker/dockerfile:1.2

# Use Python base image because installing specific Python versions is slow and hard
FROM python:3.10.1-bullseye

# Install Go
RUN curl -L https://go.dev/dl/go1.16.3.linux-amd64.tar.gz | tar -C /usr/local -xzf -
ENV GOPATH /go
ENV PATH=/usr/local/go/bin:$GOPATH/bin:$PATH
RUN go install std

# Install Docker client
RUN curl -L https://download.docker.com/linux/static/stable/x86_64/docker-20.10.9.tgz | tar -C /usr/local/bin -xzf - --strip-components=1 && \
    docker --version

RUN mkdir /src
WORKDIR /src

# Install Python development dependencies
COPY requirements-dev.txt /src
RUN --mount=type=cache,target=/root/.cache/pip pip install -r requirements-dev.txt

# Install Go dependencies
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

# Build Python package
COPY python /src/python
COPY README.md /src
RUN cd python && python setup.py bdist_wheel


# Compile and install Cog
COPY . /src
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install cmd/cog/cog.go
