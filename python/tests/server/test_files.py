import io
from unittest import mock
from unittest.mock import AsyncMock, Mock

import httpx
import pytest
from cog.server.clients import ClientManager


@pytest.mark.asyncio
async def test_upload_file():
    mock_fh = io.BytesIO()
    mock_client = AsyncMock(spec=httpx.AsyncClient)

    mock_response = Mock(spec=httpx.Response)
    mock_response.status_code = 201
    mock_response.text = ""
    mock_response.headers = {}
    mock_response.url = "http://example.com/upload/file?some-gubbins"

    mock_client.put.return_value = mock_response

    client_manager = ClientManager()
    client_manager.file_client = mock_client

    final_url = await client_manager.upload_file(
        mock_fh, url="http://example.com/upload", prediction_id=None
    )

    assert final_url == "http://example.com/upload/file"
    mock_client.put.assert_called_with(
        "http://example.com/upload/file",
        content=mock.ANY,
        headers={
            "Content-Type": "application/octet-stream",
        },
        timeout=mock.ANY,
    )


@pytest.mark.asyncio
async def test_upload_file_with_prediction_id():
    mock_fh = io.BytesIO()
    mock_client = AsyncMock(spec=httpx.AsyncClient)

    mock_response = Mock(spec=httpx.Response)
    mock_response.status_code = 201
    mock_response.text = ""
    mock_response.headers = {}
    mock_response.url = "http://example.com/upload/file?some-gubbins"

    mock_client.put.return_value = mock_response

    client_manager = ClientManager()
    client_manager.file_client = mock_client

    final_url = await client_manager.upload_file(
        mock_fh, url="http://example.com/upload", prediction_id="abc123"
    )

    assert final_url == "http://example.com/upload/file"
    mock_client.put.assert_called_with(
        "http://example.com/upload/file",
        content=mock.ANY,
        headers={
            "Content-Type": "application/octet-stream",
            "X-Prediction-ID": "abc123",
        },
        timeout=mock.ANY,
    )


@pytest.mark.asyncio
async def test_upload_file_with_location():
    mock_fh = io.BytesIO()
    mock_client = AsyncMock(spec=httpx.AsyncClient)

    mock_response = Mock(spec=httpx.Response)
    mock_response.status_code = 201
    mock_response.text = ""
    mock_response.headers = {
        "location": "http://cdn.example.com/bucket/file?some-gubbins"
    }
    mock_response.url = "http://example.com/upload/file?some-gubbins"

    mock_client.put.return_value = mock_response

    client_manager = ClientManager()
    client_manager.file_client = mock_client

    final_url = await client_manager.upload_file(
        mock_fh, url="http://example.com/upload", prediction_id="abc123"
    )

    assert final_url == "http://cdn.example.com/bucket/file"
    mock_client.put.assert_called_with(
        "http://example.com/upload/file",
        content=mock.ANY,
        headers={
            "Content-Type": "application/octet-stream",
            "X-Prediction-ID": "abc123",
        },
        timeout=mock.ANY,
    )
