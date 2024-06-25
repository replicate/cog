import io
from unittest.mock import Mock

import requests
from cog.files import put_file_to_signed_endpoint


def test_put_file_to_signed_endpoint():
    mock_fh = io.BytesIO()
    mock_client = Mock()

    mock_response = Mock(spec=requests.Response)
    mock_response.status_code = 201
    mock_response.text = ""
    mock_response.headers = {}
    mock_response.url = "http://example.com/upload/file?some-gubbins"
    mock_response.ok = True

    mock_client.put.return_value = mock_response

    final_url = put_file_to_signed_endpoint(
        mock_fh, "http://example.com/upload", mock_client, prediction_id=None
    )

    assert final_url == "http://example.com/upload/file"
    mock_client.put.assert_called_with(
        "http://example.com/upload/file",
        mock_fh,
        headers={
            "Content-Type": None,
        },
        timeout=(10, 15),
    )


def test_put_file_to_signed_endpoint_with_prediction_id():
    mock_fh = io.BytesIO()
    mock_client = Mock()

    mock_response = Mock(spec=requests.Response)
    mock_response.status_code = 201
    mock_response.text = ""
    mock_response.headers = {}
    mock_response.url = "http://example.com/upload/file?some-gubbins"
    mock_response.ok = True

    mock_client.put.return_value = mock_response

    final_url = put_file_to_signed_endpoint(
        mock_fh, "http://example.com/upload", mock_client, prediction_id="abc123"
    )

    assert final_url == "http://example.com/upload/file"
    mock_client.put.assert_called_with(
        "http://example.com/upload/file",
        mock_fh,
        headers={
            "Content-Type": None,
            "X-Prediction-ID": "abc123",
        },
        timeout=(10, 15),
    )


def test_put_file_to_signed_endpoint_with_location():
    mock_fh = io.BytesIO()
    mock_client = Mock()

    mock_response = Mock(spec=requests.Response)
    mock_response.status_code = 201
    mock_response.text = ""
    mock_response.headers = {
        "location": "http://cdn.example.com/bucket/file?some-gubbins"
    }
    mock_response.url = "http://example.com/upload/file?some-gubbins"
    mock_response.ok = True

    mock_client.put.return_value = mock_response

    final_url = put_file_to_signed_endpoint(
        mock_fh, "http://example.com/upload", mock_client, prediction_id="abc123"
    )

    assert final_url == "http://cdn.example.com/bucket/file"
    mock_client.put.assert_called_with(
        "http://example.com/upload/file",
        mock_fh,
        headers={
            "Content-Type": None,
            "X-Prediction-ID": "abc123",
        },
        timeout=(10, 15),
    )
