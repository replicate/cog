package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const installJid = `
RUN pip install flask
RUN echo aW1wb3J0IHRyYWNlYmFjawpmcm9tIGFiYyBpbXBvcnQgQUJDLCBhYnN0cmFjdG1ldGhvZApmcm9tIHBhdGhsaWIgaW1wb3J0IFBhdGgKCmZyb20gZmxhc2sgaW1wb3J0IEZsYXNrLCBzZW5kX2ZpbGUsIHJlcXVlc3QsIGpzb25pZnkKCgpjbGFzcyBNb2RlbChBQkMpOgogICAgQGFic3RyYWN0bWV0aG9kCiAgICBkZWYgc2V0dXAoc2VsZik6CiAgICAgICAgcGFzcwoKICAgIEBhYnN0cmFjdG1ldGhvZAogICAgZGVmIHJ1bihzZWxmLCAqKmt3YXJncyk6CiAgICAgICAgcGFzcwoKICAgIGRlZiBjbGlfcnVuKHNlbGYpOgogICAgICAgIHNlbGYuc2V0dXAoKQogICAgICAgIHJlc3VsdCA9IHNlbGYucnVuKCkKICAgICAgICBwcmludChyZXN1bHQpCgogICAgZGVmIHN0YXJ0X3NlcnZlcihzZWxmKToKICAgICAgICBzZWxmLnNldHVwKCkKICAgICAgICBhcHAgPSBGbGFzayhfX25hbWVfXykKCiAgICAgICAgQGFwcC5yb3V0ZSgiL2luZmVyIiwgbWV0aG9kcz1bIlBPU1QiXSkKICAgICAgICBkZWYgaGFuZGxlX3JlcXVlc3QoKToKICAgICAgICAgICAgYXJncyA9IHJlcXVlc3QuZm9ybQogICAgICAgICAgICByZXN1bHQgPSBzZWxmLnJ1bigqKmFyZ3MpCiAgICAgICAgICAgIHJldHVybiBzZWxmLmNyZWF0ZV9yZXNwb25zZShyZXN1bHQpCgogICAgICAgIEBhcHAucm91dGUoIi9pbmZlci1haS1wbGF0Zm9ybSIsIG1ldGhvZHM9WyJQT1NUIl0pCiAgICAgICAgZGVmIGhhbmRsZV9haV9wbGF0Zm9ybV9yZXF1ZXN0KCk6CiAgICAgICAgICAgIHRyeToKICAgICAgICAgICAgICAgIGNvbnRlbnQgPSByZXF1ZXN0Lmpzb24KICAgICAgICAgICAgICAgIGluc3RhbmNlcyA9IGNvbnRlbnRbImluc3RhbmNlcyJdCiAgICAgICAgICAgICAgICByZXN1bHRzID0gW10KICAgICAgICAgICAgICAgIGZvciBpbnN0YW5jZSBpbiBpbnN0YW5jZXM6CiAgICAgICAgICAgICAgICAgICAgcmVzdWx0cy5hcHBlbmQoc2VsZi5ydW4oKippbnN0YW5jZSkpCiAgICAgICAgICAgICAgICByZXR1cm4ganNvbmlmeSh7InByZWRpY3Rpb25zIjogcmVzdWx0cyx9KQogICAgICAgICAgICBleGNlcHQgRXhjZXB0aW9uIGFzIGU6CiAgICAgICAgICAgICAgICB0YiA9IHRyYWNlYmFjay5mb3JtYXRfZXhjKCkKICAgICAgICAgICAgICAgIHJldHVybiBqc29uaWZ5KHsiZXJyb3IiOiB0Yix9KQoKICAgICAgICBAYXBwLnJvdXRlKCIvcGluZyIpCiAgICAgICAgZGVmIHBpbmcoKToKICAgICAgICAgICAgcmV0dXJuICJQT05HIgoKCiAgICAgICAgYXBwLnJ1bihob3N0PSIwLjAuMC4wIiwgcG9ydD01MDAwKQoKICAgIGRlZiBjcmVhdGVfcmVzcG9uc2Uoc2VsZiwgcmVzdWx0KToKICAgICAgICBpZiBpc2luc3RhbmNlKHJlc3VsdCwgUGF0aCk6CiAgICAgICAgICAgIHJldHVybiBzZW5kX2ZpbGUoc3RyKHJlc3VsdCkpCiAgICAgICAgZWxpZiBpc2luc3RhbmNlKHJlc3VsdCwgc3RyKToKICAgICAgICAgICAgcmV0dXJuIHJlc3VsdAo= | base64 --decode > /usr/local/lib/python3.8/dist-packages/jid.py`

func TestGenerateEmpty(t *testing.T) {
	conf, err := ConfigFromYAML([]byte{})
	require.NoError(t, err)

	expectedCPU := `FROM ubuntu:20.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
	&& apt-get install -y --no-install-recommends software-properties-common \
	&& add-apt-repository -y ppa:deadsnakes/ppa \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends python3.8 python3-pip \
	&& apt-get purge -y --auto-remove software-properties-common \
	&& rm -rf /var/lib/apt/lists/* \
	&& ln -s /usr/bin/python3.8 /usr/bin/python \
	&& ln -s /usr/bin/pip3 /usr/bin/pip` + installJid

	expectedGPU := `FROM nvidia/cuda:11.0-devel-ubuntu20.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
	&& apt-get install -y --no-install-recommends software-properties-common \
	&& add-apt-repository -y ppa:deadsnakes/ppa \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends python3.8 python3-pip \
	&& apt-get purge -y --auto-remove software-properties-common \
	&& rm -rf /var/lib/apt/lists/* \
	&& ln -s /usr/bin/python3.8 /usr/bin/python \
	&& ln -s /usr/bin/pip3 /usr/bin/pip` + installJid

	gen := DockerfileGenerator{conf, "cpu"}
	actualCPU, err := gen.Generate()
	require.NoError(t, err)
	gen = DockerfileGenerator{conf, "gpu"}
	actualGPU, err := gen.Generate()
	require.NoError(t, err)

	require.Equal(t, expectedCPU, actualCPU)
	require.Equal(t, expectedGPU, actualGPU)
}

func TestGenerateFull(t *testing.T) {
	conf, err := ConfigFromYAML([]byte(`
environment:
  python_requirements: my-requirements.txt
  python_packages:
    - torch==1.5.1
    - pandas==1.2.0.12
  system_packages:
    - ffmpeg
    - cowsay
`))
	require.NoError(t, err)

	expectedCPU := `FROM ubuntu:20.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y ffmpeg cowsay && rm -rf /var/lib/apt/lists/*
RUN apt-get update \
	&& apt-get install -y --no-install-recommends software-properties-common \
	&& add-apt-repository -y ppa:deadsnakes/ppa \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends python3.8 python3-pip \
	&& apt-get purge -y --auto-remove software-properties-common \
	&& rm -rf /var/lib/apt/lists/* \
	&& ln -s /usr/bin/python3.8 /usr/bin/python \
	&& ln -s /usr/bin/pip3 /usr/bin/pip
COPY my-requirements.txt /tmp/requirements.txt
RUN pip install -r /tmp/requirements.txt && rm /tmp/requirements.txt
RUN pip install torch==1.5.1 pandas==1.2.0.12` + installJid

	expectedGPU := `FROM nvidia/cuda:11.0-devel-ubuntu20.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y ffmpeg cowsay && rm -rf /var/lib/apt/lists/*
RUN apt-get update \
	&& apt-get install -y --no-install-recommends software-properties-common \
	&& add-apt-repository -y ppa:deadsnakes/ppa \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends python3.8 python3-pip \
	&& apt-get purge -y --auto-remove software-properties-common \
	&& rm -rf /var/lib/apt/lists/* \
	&& ln -s /usr/bin/python3.8 /usr/bin/python \
	&& ln -s /usr/bin/pip3 /usr/bin/pip
COPY my-requirements.txt /tmp/requirements.txt
RUN pip install -r /tmp/requirements.txt && rm /tmp/requirements.txt
RUN pip install torch==1.5.1 pandas==1.2.0.12` + installJid

	gen := DockerfileGenerator{conf, "cpu"}
	actualCPU, err := gen.Generate()
	require.NoError(t, err)
	gen = DockerfileGenerator{conf, "gpu"}
	actualGPU, err := gen.Generate()
	require.NoError(t, err)

	require.Equal(t, expectedCPU, actualCPU)
	require.Equal(t, expectedGPU, actualGPU)
}
