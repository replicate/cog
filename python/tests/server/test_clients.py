from email.message import Message
import io
import os
import tempfile
from urllib.response import addinfourl
from unittest import mock

import cog
import httpx
import pytest
from cog.server.clients import ClientManager


@pytest.mark.asyncio
async def test_upload_files_without_url():
    client_manager = ClientManager()
    temp_dir = tempfile.mkdtemp()
    temp_path = os.path.join(temp_dir, "my_file.txt")
    with open(temp_path, "w") as fh:
        fh.write("file content")
    obj = {"path": cog.Path(temp_path)}
    result = await client_manager.upload_files(obj, url=None, prediction_id=None)
    assert result == {"path": "data:text/plain;base64,ZmlsZSBjb250ZW50"}


@pytest.mark.asyncio
@pytest.mark.respx(base_url="https://example.com")
async def test_upload_files_with_url(respx_mock):
    uploader = respx_mock.put("/bucket/my_file.txt").mock(
        return_value=httpx.Response(201)
    )

    client_manager = ClientManager()
    temp_dir = tempfile.mkdtemp()
    temp_path = os.path.join(temp_dir, "my_file.txt")
    with open(temp_path, "w") as fh:
        fh.write("file content")

    obj = {"path": cog.Path(temp_path)}
    result = await client_manager.upload_files(
        obj, url="https://example.com/bucket", prediction_id=None
    )
    assert result == {"path": "https://example.com/bucket/my_file.txt"}

    assert uploader.call_count == 1


@pytest.mark.asyncio
@pytest.mark.respx(base_url="https://example.com")
async def test_upload_files_with_prediction_id(respx_mock):
    uploader = respx_mock.put(
        "/bucket/my_file.txt", headers={"x-prediction-id": "p123"}
    ).mock(return_value=httpx.Response(201))

    client_manager = ClientManager()
    temp_dir = tempfile.mkdtemp()
    temp_path = os.path.join(temp_dir, "my_file.txt")
    with open(temp_path, "w") as fh:
        fh.write("file content")

    obj = {"path": cog.Path(temp_path)}
    result = await client_manager.upload_files(
        obj, url="https://example.com/bucket", prediction_id="p123"
    )
    assert result == {"path": "https://example.com/bucket/my_file.txt"}

    assert uploader.call_count == 1


@pytest.mark.asyncio
@pytest.mark.respx(base_url="https://example.com")
async def test_upload_files_with_location_header(respx_mock):
    uploader = respx_mock.put("/bucket/my_file.txt").mock(
        return_value=httpx.Response(
            201, headers={"Location": "https://cdn.example.com/bucket/my_file.txt"}
        )
    )

    client_manager = ClientManager()
    temp_dir = tempfile.mkdtemp()
    temp_path = os.path.join(temp_dir, "my_file.txt")
    with open(temp_path, "w") as fh:
        fh.write("file content")

    obj = {"path": cog.Path(temp_path)}
    result = await client_manager.upload_files(
        obj, url="https://example.com/bucket", prediction_id=None
    )
    assert result == {"path": "https://cdn.example.com/bucket/my_file.txt"}

    assert uploader.call_count == 1


@pytest.mark.asyncio
@pytest.mark.respx(base_url="https://example.com")
async def test_upload_files_with_retry(respx_mock):
    uploader = respx_mock.put("/bucket/my_file.txt").mock(
        return_value=httpx.Response(502)
    )

    client_manager = ClientManager()
    temp_dir = tempfile.mkdtemp()
    temp_path = os.path.join(temp_dir, "my_file.txt")
    with open(temp_path, "w") as fh:
        fh.write("file content")

    obj = {"path": cog.Path(temp_path)}
    with pytest.raises(httpx.HTTPStatusError):
        await client_manager.upload_files(
            obj, url="https://example.com/bucket", prediction_id=None
        )

    assert uploader.call_count == 3


@pytest.mark.asyncio
@pytest.mark.respx(base_url="https://example.com")
@mock.patch("urllib.request.urlopen")
async def test_upload_files_with_url_file(urlopen_mock, respx_mock):
    fp = io.BytesIO(b"hello world")
    urlopen_mock.return_value = addinfourl(
        fp=fp, headers=Message(), url="https://example.com/cdn/my_file.txt"
    )

    uploader = respx_mock.put("/bucket/my_file.txt").mock(
        return_value=httpx.Response(
            201, headers={"Location": "https://cdn.example.com/bucket/my_file.txt"}
        )
    )

    client_manager = ClientManager()

    obj = {"path": cog.types.URLFile("https://example.com/cdn/my_file.txt")}
    result = await client_manager.upload_files(
        obj, url="https://example.com/bucket", prediction_id=None
    )
    assert result == {"path": "https://cdn.example.com/bucket/my_file.txt"}

    assert uploader.call_count == 1
    assert urlopen_mock.call_count == 1
    assert urlopen_mock.call_args[0][0] == "https://example.com/cdn/my_file.txt"


@pytest.mark.asyncio
@pytest.mark.respx(base_url="https://example.com")
@mock.patch("urllib.request.urlopen")
async def test_upload_files_with_url_file_with_retry(urlopen_mock, respx_mock):
    fp = io.BytesIO(b"hello world")
    urlopen_mock.return_value = addinfourl(
        fp=fp, headers=Message(), url="https://example.com/cdn/my_file.txt"
    )

    uploader = respx_mock.put("/bucket/my_file.txt").mock(
        return_value=httpx.Response(502)
    )

    client_manager = ClientManager()

    obj = {"path": cog.types.URLFile("https://example.com/cdn/my_file.txt")}
    with pytest.raises(httpx.HTTPStatusError):
        await client_manager.upload_files(
            obj, url="https://example.com/bucket", prediction_id=None
        )

    assert uploader.call_count == 3
    assert urlopen_mock.call_count == 1
    assert urlopen_mock.call_args[0][0] == "https://example.com/cdn/my_file.txt"
