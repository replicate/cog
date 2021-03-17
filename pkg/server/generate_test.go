package server

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/replicate/cog/pkg/model"
)

const installCog = `
RUN pip install flask
RUN echo aW1wb3J0IG9zCmltcG9ydCBzaHV0aWwKaW1wb3J0IHRlbXBmaWxlCmZyb20gZGF0YWNsYXNzZXMgaW1wb3J0IGRhdGFjbGFzcwppbXBvcnQgaW5zcGVjdAppbXBvcnQgZnVuY3Rvb2xzCmltcG9ydCB0cmFjZWJhY2sKZnJvbSBhYmMgaW1wb3J0IEFCQywgYWJzdHJhY3RtZXRob2QKZnJvbSBwYXRobGliIGltcG9ydCBQYXRoCmZyb20gdHlwaW5nIGltcG9ydCBPcHRpb25hbCwgQW55LCBUeXBlLCBMaXN0LCBDYWxsYWJsZSwgRGljdAoKZnJvbSBmbGFzayBpbXBvcnQgRmxhc2ssIHNlbmRfZmlsZSwgcmVxdWVzdCwganNvbmlmeSwgYWJvcnQKZnJvbSB3ZXJremV1Zy5kYXRhc3RydWN0dXJlcyBpbXBvcnQgRmlsZVN0b3JhZ2UKCl9WQUxJRF9JTlBVVF9UWVBFUyA9IGZyb3plbnNldChbc3RyLCBpbnQsIFBhdGhdKQoKCmNsYXNzIElucHV0VmFsaWRhdGlvbkVycm9yKEV4Y2VwdGlvbik6CiAgICBwYXNzCgoKY2xhc3MgTW9kZWwoQUJDKToKICAgIGFwcDogRmxhc2sKCiAgICBAYWJzdHJhY3RtZXRob2QKICAgIGRlZiBzZXR1cChzZWxmKToKICAgICAgICBwYXNzCgogICAgQGFic3RyYWN0bWV0aG9kCiAgICBkZWYgcnVuKHNlbGYsICoqa3dhcmdzKToKICAgICAgICBwYXNzCgogICAgZGVmIGNsaV9ydW4oc2VsZik6CiAgICAgICAgc2VsZi5zZXR1cCgpCiAgICAgICAgcmVzdWx0ID0gc2VsZi5ydW4oKQogICAgICAgIHByaW50KHJlc3VsdCkKCiAgICBkZWYgbWFrZV9hcHAoc2VsZikgLT4gRmxhc2s6CiAgICAgICAgc2VsZi5zZXR1cCgpCiAgICAgICAgYXBwID0gRmxhc2soX19uYW1lX18pCgogICAgICAgIEBhcHAucm91dGUoIi9pbmZlciIsIG1ldGhvZHM9WyJQT1NUIl0pCiAgICAgICAgZGVmIGhhbmRsZV9yZXF1ZXN0KCk6CiAgICAgICAgICAgIGNsZWFudXBfZnVuY3Rpb25zID0gW10KICAgICAgICAgICAgdHJ5OgogICAgICAgICAgICAgICAgcmF3X2lucHV0cyA9IHt9CiAgICAgICAgICAgICAgICBmb3Iga2V5LCB2YWwgaW4gcmVxdWVzdC5mb3JtLml0ZW1zKCk6CiAgICAgICAgICAgICAgICAgICAgcmF3X2lucHV0c1trZXldID0gdmFsCiAgICAgICAgICAgICAgICBmb3Iga2V5LCB2YWwgaW4gcmVxdWVzdC5maWxlcy5pdGVtcygpOgogICAgICAgICAgICAgICAgICAgIGlmIGtleSBpbiByYXdfaW5wdXRzOgogICAgICAgICAgICAgICAgICAgICAgICByZXR1cm4gYWJvcnQoCiAgICAgICAgICAgICAgICAgICAgICAgICAgICA0MDAsIGYiRHVwbGljYXRlZCBhcmd1bWVudCBuYW1lIGluIGZvcm0gYW5kIGZpbGVzOiB7a2V5fSIKICAgICAgICAgICAgICAgICAgICAgICAgKQogICAgICAgICAgICAgICAgICAgIHJhd19pbnB1dHNba2V5XSA9IHZhbAoKICAgICAgICAgICAgICAgIGlmIGhhc2F0dHIoc2VsZi5ydW4sICJfaW5wdXRzIik6CiAgICAgICAgICAgICAgICAgICAgdHJ5OgogICAgICAgICAgICAgICAgICAgICAgICBpbnB1dHMgPSBzZWxmLnZhbGlkYXRlX2FuZF9jb252ZXJ0X2lucHV0cygKICAgICAgICAgICAgICAgICAgICAgICAgICAgIHJhd19pbnB1dHMsIGNsZWFudXBfZnVuY3Rpb25zCiAgICAgICAgICAgICAgICAgICAgICAgICkKICAgICAgICAgICAgICAgICAgICBleGNlcHQgSW5wdXRWYWxpZGF0aW9uRXJyb3IgYXMgZToKICAgICAgICAgICAgICAgICAgICAgICAgcmV0dXJuIGFib3J0KDQwMCwgc3RyKGUpKQogICAgICAgICAgICAgICAgZWxzZToKICAgICAgICAgICAgICAgICAgICBpbnB1dHMgPSByYXdfaW5wdXRzCgogICAgICAgICAgICAgICAgcmVzdWx0ID0gc2VsZi5ydW4oKippbnB1dHMpCiAgICAgICAgICAgICAgICByZXR1cm4gc2VsZi5jcmVhdGVfcmVzcG9uc2UocmVzdWx0KQogICAgICAgICAgICBmaW5hbGx5OgogICAgICAgICAgICAgICAgZm9yIGNsZWFudXBfZnVuY3Rpb24gaW4gY2xlYW51cF9mdW5jdGlvbnM6CiAgICAgICAgICAgICAgICAgICAgY2xlYW51cF9mdW5jdGlvbigpCgogICAgICAgIEBhcHAucm91dGUoIi9waW5nIikKICAgICAgICBkZWYgcGluZygpOgogICAgICAgICAgICByZXR1cm4gIlBPTkciCgogICAgICAgIEBhcHAucm91dGUoIi9oZWxwIikKICAgICAgICBkZWYgaGVscCgpOgogICAgICAgICAgICBhcmdzID0ge30KICAgICAgICAgICAgaWYgaGFzYXR0cihzZWxmLnJ1biwgIl9pbnB1dHMiKToKICAgICAgICAgICAgICAgIGlucHV0X3NwZWNzID0gc2VsZi5ydW4uX2lucHV0cwogICAgICAgICAgICAgICAgZm9yIG5hbWUsIHNwZWMgaW4gaW5wdXRfc3BlY3MuaXRlbXMoKToKICAgICAgICAgICAgICAgICAgICBhcmcgPSB7CiAgICAgICAgICAgICAgICAgICAgICAgICJ0eXBlIjogX3R5cGVfbmFtZShzcGVjLnR5cGUpLAogICAgICAgICAgICAgICAgICAgIH0KICAgICAgICAgICAgICAgICAgICBpZiBzcGVjLmhlbHA6CiAgICAgICAgICAgICAgICAgICAgICAgIGFyZ1siaGVscCJdID0gc3BlYy5oZWxwCiAgICAgICAgICAgICAgICAgICAgaWYgc3BlYy5kZWZhdWx0OgogICAgICAgICAgICAgICAgICAgICAgICBhcmdbImRlZmF1bHQiXSA9IHNwZWMuZGVmYXVsdAogICAgICAgICAgICAgICAgICAgIGFyZ3NbbmFtZV0gPSBhcmcKICAgICAgICAgICAgcmV0dXJuIGpzb25pZnkoeyJhcmd1bWVudHMiOiBhcmdzfSkKCiAgICAgICAgcmV0dXJuIGFwcAoKICAgIGRlZiBzdGFydF9zZXJ2ZXIoc2VsZik6CiAgICAgICAgYXBwID0gc2VsZi5tYWtlX2FwcCgpCiAgICAgICAgYXBwLnJ1bihob3N0PSIwLjAuMC4wIiwgcG9ydD01MDAwKQoKICAgIGRlZiBjcmVhdGVfcmVzcG9uc2Uoc2VsZiwgcmVzdWx0KToKICAgICAgICBpZiBpc2luc3RhbmNlKHJlc3VsdCwgUGF0aCk6CiAgICAgICAgICAgIHJldHVybiBzZW5kX2ZpbGUoc3RyKHJlc3VsdCkpCiAgICAgICAgZWxpZiBpc2luc3RhbmNlKHJlc3VsdCwgc3RyKToKICAgICAgICAgICAgcmV0dXJuIHJlc3VsdAoKICAgIGRlZiB2YWxpZGF0ZV9hbmRfY29udmVydF9pbnB1dHMoCiAgICAgICAgc2VsZiwgcmF3X2lucHV0czogRGljdFtzdHIsIEFueV0sIGNsZWFudXBfZnVuY3Rpb25zOiBMaXN0W0NhbGxhYmxlXQogICAgKSAtPiBEaWN0W3N0ciwgQW55XToKICAgICAgICBpbnB1dF9zcGVjcyA9IHNlbGYucnVuLl9pbnB1dHMKICAgICAgICBpbnB1dHMgPSB7fQoKICAgICAgICBmb3IgbmFtZSwgaW5wdXRfc3BlYyBpbiBpbnB1dF9zcGVjcy5pdGVtcygpOgogICAgICAgICAgICBpZiBuYW1lIGluIHJhd19pbnB1dHM6CiAgICAgICAgICAgICAgICB2YWwgPSByYXdfaW5wdXRzW25hbWVdCgogICAgICAgICAgICAgICAgaWYgaW5wdXRfc3BlYy50eXBlID09IFBhdGg6CiAgICAgICAgICAgICAgICAgICAgaWYgbm90IGlzaW5zdGFuY2UodmFsLCBGaWxlU3RvcmFnZSk6CiAgICAgICAgICAgICAgICAgICAgICAgIHJhaXNlIElucHV0VmFsaWRhdGlvbkVycm9yKAogICAgICAgICAgICAgICAgICAgICAgICAgICAgZiJDb3VsZCBub3QgY29udmVydCBmaWxlIGlucHV0IHtuYW1lfSB0byB7X3R5cGVfbmFtZShpbnB1dF9zcGVjLnR5cGUpfSIsCiAgICAgICAgICAgICAgICAgICAgICAgICkKICAgICAgICAgICAgICAgICAgICBpZiB2YWwuZmlsZW5hbWUgaXMgTm9uZToKICAgICAgICAgICAgICAgICAgICAgICAgcmFpc2UgSW5wdXRWYWxpZGF0aW9uRXJyb3IoCiAgICAgICAgICAgICAgICAgICAgICAgICAgICBmIk5vIGZpbGVuYW1lIGlzIHByb3ZpZGVkIGZvciBmaWxlIGlucHV0IHtuYW1lfSIKICAgICAgICAgICAgICAgICAgICAgICAgKQoKICAgICAgICAgICAgICAgICAgICB0ZW1wX2RpciA9IHRlbXBmaWxlLm1rZHRlbXAoKQogICAgICAgICAgICAgICAgICAgIGNsZWFudXBfZnVuY3Rpb25zLmFwcGVuZChsYW1iZGE6IHNodXRpbC5ybXRyZWUodGVtcF9kaXIpKQoKICAgICAgICAgICAgICAgICAgICB0ZW1wX3BhdGggPSBvcy5wYXRoLmpvaW4odGVtcF9kaXIsIHZhbC5maWxlbmFtZSkKICAgICAgICAgICAgICAgICAgICB3aXRoIG9wZW4odGVtcF9wYXRoLCAid2IiKSBhcyBmOgogICAgICAgICAgICAgICAgICAgICAgICBmLndyaXRlKHZhbC5zdHJlYW0ucmVhZCgpKQogICAgICAgICAgICAgICAgICAgIGNvbnZlcnRlZCA9IFBhdGgodGVtcF9wYXRoKQoKICAgICAgICAgICAgICAgIGVsaWYgaW5wdXRfc3BlYy50eXBlID09IGludDoKICAgICAgICAgICAgICAgICAgICB0cnk6CiAgICAgICAgICAgICAgICAgICAgICAgIGNvbnZlcnRlZCA9IGludCh2YWwpCiAgICAgICAgICAgICAgICAgICAgZXhjZXB0IFZhbHVlRXJyb3I6CiAgICAgICAgICAgICAgICAgICAgICAgIHJhaXNlIElucHV0VmFsaWRhdGlvbkVycm9yKAogICAgICAgICAgICAgICAgICAgICAgICAgICAgZiJDb3VsZCBub3QgY29udmVydCB7bmFtZX09e3ZhbH0gdG8gaW50IgogICAgICAgICAgICAgICAgICAgICAgICApCgogICAgICAgICAgICAgICAgZWxpZiBpbnB1dF9zcGVjLnR5cGUgPT0gc3RyOgogICAgICAgICAgICAgICAgICAgIGlmIGlzaW5zdGFuY2UodmFsLCBGaWxlU3RvcmFnZSk6CiAgICAgICAgICAgICAgICAgICAgICAgIHJhaXNlIElucHV0VmFsaWRhdGlvbkVycm9yKAogICAgICAgICAgICAgICAgICAgICAgICAgICAgZiJDb3VsZCBub3QgY29udmVydCBmaWxlIGlucHV0IHtuYW1lfSB0byBzdHIiCiAgICAgICAgICAgICAgICAgICAgICAgICkKICAgICAgICAgICAgICAgICAgICBjb252ZXJ0ZWQgPSB2YWwKCiAgICAgICAgICAgICAgICBlbHNlOgogICAgICAgICAgICAgICAgICAgIHJhaXNlIFR5cGVFcnJvcihmIkludGVybmFsIGVycm9yOiBJbnB1dCB0eXBlIHtpbnB1dF9zcGVjfSBpcyBub3QgYSB2YWxpZCBpbnB1dCB0eXBlIikKCiAgICAgICAgICAgIGVsc2U6CiAgICAgICAgICAgICAgICBpZiBpbnB1dF9zcGVjLmRlZmF1bHQgaXMgbm90IE5vbmU6CiAgICAgICAgICAgICAgICAgICAgY29udmVydGVkID0gaW5wdXRfc3BlYy5kZWZhdWx0CiAgICAgICAgICAgICAgICBlbHNlOgogICAgICAgICAgICAgICAgICAgIHJhaXNlIElucHV0VmFsaWRhdGlvbkVycm9yKGYiTWlzc2luZyBleHBlY3RlZCBhcmd1bWVudDoge25hbWV9IikKICAgICAgICAgICAgaW5wdXRzW25hbWVdID0gY29udmVydGVkCgogICAgICAgIGV4cGVjdGVkX2tleXMgPSBzZXQoc2VsZi5ydW4uX2lucHV0cy5rZXlzKCkpCiAgICAgICAgcmF3X2tleXMgPSBzZXQocmF3X2lucHV0cy5rZXlzKCkpCiAgICAgICAgZXh0cmFuZW91c19rZXlzID0gcmF3X2tleXMgLSBleHBlY3RlZF9rZXlzCiAgICAgICAgaWYgZXh0cmFuZW91c19rZXlzOgogICAgICAgICAgICByYWlzZSBJbnB1dFZhbGlkYXRpb25FcnJvcigKICAgICAgICAgICAgICAgIGYiRXh0cmFuZW91cyBpbnB1dCBrZXlzOiB7JywgJy5qb2luKGV4dHJhbmVvdXNfa2V5cyl9IgogICAgICAgICAgICApCgogICAgICAgIHJldHVybiBpbnB1dHMKCgpAZGF0YWNsYXNzCmNsYXNzIElucHV0U3BlYzoKICAgIHR5cGU6IFR5cGUKICAgIGRlZmF1bHQ6IE9wdGlvbmFsW0FueV0gPSBOb25lCiAgICBoZWxwOiBPcHRpb25hbFtzdHJdID0gTm9uZQoKCmRlZiBpbnB1dChuYW1lLCB0eXBlLCBkZWZhdWx0PU5vbmUsIGhlbHA9Tm9uZSk6CiAgICBpZiB0eXBlIG5vdCBpbiBfVkFMSURfSU5QVVRfVFlQRVM6CiAgICAgICAgdHlwZV9uYW1lID0gX3R5cGVfbmFtZSh0eXBlKQogICAgICAgIHR5cGVfbGlzdCA9ICIsICIuam9pbihbX3R5cGVfbmFtZSh0KSBmb3IgdCBpbiBfVkFMSURfSU5QVVRfVFlQRVNdKQogICAgICAgIHJhaXNlIFZhbHVlRXJyb3IoCiAgICAgICAgICAgIGYie3R5cGVfbmFtZX0gaXMgbm90IGEgdmFsaWQgaW5wdXQgdHlwZS4gVmFsaWQgdHlwZXMgYXJlOiB7dHlwZV9saXN0fSIKICAgICAgICApCgogICAgZGVmIHdyYXBwZXIoZik6CiAgICAgICAgaWYgbm90IGhhc2F0dHIoZiwgIl9pbnB1dHMiKToKICAgICAgICAgICAgZi5faW5wdXRzID0ge30KCiAgICAgICAgaWYgbmFtZSBpbiBmLl9pbnB1dHM6CiAgICAgICAgICAgIHJhaXNlIFZhbHVlRXJyb3IoZiJ7bmFtZX0gaXMgYWxyZWFkeSBkZWZpbmVkIGFzIGFuIGFyZ3VtZW50IikKCiAgICAgICAgaWYgdHlwZSA9PSBQYXRoIGFuZCBkZWZhdWx0IGlzIG5vdCBOb25lOgogICAgICAgICAgICByYWlzZSBUeXBlRXJyb3IoIkNhbm5vdCB1c2UgZGVmYXVsdCB3aXRoIFBhdGggdHlwZSIpCgogICAgICAgIGYuX2lucHV0c1tuYW1lXSA9IElucHV0U3BlYyh0eXBlPXR5cGUsIGRlZmF1bHQ9ZGVmYXVsdCwgaGVscD1oZWxwKQoKICAgICAgICBAZnVuY3Rvb2xzLndyYXBzKGYpCiAgICAgICAgZGVmIHdyYXBzKHNlbGYsICoqa3dhcmdzKToKICAgICAgICAgICAgaWYgbm90IGlzaW5zdGFuY2Uoc2VsZiwgTW9kZWwpOgogICAgICAgICAgICAgICAgcmFpc2UgVHlwZUVycm9yKCJ7c2VsZn0gaXMgbm90IGFuIGluc3RhbmNlIG9mIGNvZy5Nb2RlbCIpCiAgICAgICAgICAgIHJldHVybiBmKHNlbGYsICoqa3dhcmdzKQoKICAgICAgICByZXR1cm4gd3JhcHMKCiAgICByZXR1cm4gd3JhcHBlcgoKCmRlZiBfdHlwZV9uYW1lKHR5cGU6IFR5cGUpIC0+IHN0cjoKICAgIGlmIHR5cGUgPT0gc3RyOgogICAgICAgIHJldHVybiAic3RyIgogICAgaWYgdHlwZSA9PSBpbnQ6CiAgICAgICAgcmV0dXJuICJpbnQiCiAgICBpZiB0eXBlID09IFBhdGg6CiAgICAgICAgcmV0dXJuICJQYXRoIgogICAgcmV0dXJuIHN0cih0eXBlKQoKCmRlZiBfbWV0aG9kX2FyZ19uYW1lcyhmKSAtPiBMaXN0W3N0cl06CiAgICByZXR1cm4gaW5zcGVjdC5nZXRmdWxsYXJnc3BlYyhmKVswXVsxOl0gICMgMCBpcyBzZWxmCg== | base64 --decode > /usr/local/lib/python3.8/dist-packages/cog.py`

func TestGenerateEmpty(t *testing.T) {
	conf, err := model.ConfigFromYAML([]byte(`
model: infer.py:Model
`))
	require.NoError(t, err)

	expectedCPU := `FROM ubuntu:20.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*
RUN apt-get update \
	&& apt-get install -y --no-install-recommends software-properties-common \
	&& add-apt-repository -y ppa:deadsnakes/ppa \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends python3.8 python3-pip \
	&& apt-get purge -y --auto-remove software-properties-common \
	&& rm -rf /var/lib/apt/lists/* \
	&& ln -s /usr/bin/python3.8 /usr/bin/python \
	&& ln -s /usr/bin/pip3 /usr/bin/pip` + installCog + `
COPY . /code
WORKDIR /code
CMD ["python", "-c", "from infer import Model; Model().start_server()"]`

	expectedGPU := `FROM nvidia/cuda:11.0-devel-ubuntu20.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*
RUN apt-get update \
	&& apt-get install -y --no-install-recommends software-properties-common \
	&& add-apt-repository -y ppa:deadsnakes/ppa \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends python3.8 python3-pip \
	&& apt-get purge -y --auto-remove software-properties-common \
	&& rm -rf /var/lib/apt/lists/* \
	&& ln -s /usr/bin/python3.8 /usr/bin/python \
	&& ln -s /usr/bin/pip3 /usr/bin/pip` + installCog + `
COPY . /code
WORKDIR /code
CMD ["python", "-c", "from infer import Model; Model().start_server()"]`

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
	conf, err := model.ConfigFromYAML([]byte(`
environment:
  python_requirements: my-requirements.txt
  python_packages:
    - torch==1.5.1
    - pandas==1.2.0.12
  system_packages:
    - ffmpeg
    - cowsay
model: infer.py:Model
`))
	require.NoError(t, err)

	err = conf.ValidateAndCompleteConfig()
	require.NoError(t, err)

	expectedCPU := `FROM ubuntu:20.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y ffmpeg cowsay curl && rm -rf /var/lib/apt/lists/*
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
RUN pip install -f https://download.pytorch.org/whl/torch_stable.html torch==1.5.1+cpu pandas==1.2.0.12` + installCog + `
COPY . /code
WORKDIR /code
CMD ["python", "-c", "from infer import Model; Model().start_server()"]`

	expectedGPU := `FROM nvidia/cuda:11.0-devel-ubuntu20.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y ffmpeg cowsay curl && rm -rf /var/lib/apt/lists/*
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
RUN pip install torch==1.5.1 pandas==1.2.0.12` + installCog + `
COPY . /code
WORKDIR /code
CMD ["python", "-c", "from infer import Model; Model().start_server()"]`

	gen := DockerfileGenerator{conf, "cpu"}
	actualCPU, err := gen.Generate()
	require.NoError(t, err)
	gen = DockerfileGenerator{conf, "gpu"}
	actualGPU, err := gen.Generate()
	require.NoError(t, err)

	require.Equal(t, expectedCPU, actualCPU)
	require.Equal(t, expectedGPU, actualGPU)
}
