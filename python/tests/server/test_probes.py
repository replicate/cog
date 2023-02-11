import logging
import os
import tempfile
from unittest import mock

import pytest
from cog.server.probes import ProbeHelper


@pytest.fixture
def tmpdir():
    with tempfile.TemporaryDirectory() as td:
        yield td


@mock.patch.dict(os.environ, {"KUBERNETES_SERVICE_HOST": "0.0.0.0"})
def test_ready(tmpdir):
    p = ProbeHelper(root=tmpdir)

    p.ready()

    assert os.path.isfile(os.path.join(tmpdir, "ready"))


def test_does_nothing_when_not_in_k8s(tmpdir, caplog):
    with caplog.at_level(logging.INFO):
        p = ProbeHelper(root=tmpdir)
        p.ready()

    assert os.listdir(tmpdir) == []
    assert "disabling probe helpers" in caplog.text


@mock.patch.dict(os.environ, {"KUBERNETES_SERVICE_HOST": "0.0.0.0"})
def test_creates_probe_dir_if_needed(tmpdir):
    root = os.path.join(tmpdir, "probes")
    p = ProbeHelper(root=root)

    p.ready()

    assert os.path.isdir(os.path.join(tmpdir, "probes"))
    assert os.path.isfile(os.path.join(tmpdir, "probes", "ready"))


@mock.patch.dict(os.environ, {"KUBERNETES_SERVICE_HOST": "0.0.0.0"})
def test_no_exception_when_probe_dir_exists(tmpdir, caplog):
    root = os.path.join(tmpdir, "probes")

    # Create a file
    open(root, "a").close()

    p = ProbeHelper(root=root)
    p.ready()

    assert "Failed to create cog runtime state directory" in caplog.text
