import base64
import io

import responses
from responses.matchers import multipart_matcher

from .conftest import uses_predictor, uses_predictor_with_client_options


@uses_predictor("output_wrong_type")
def test_return_wrong_type(client):
    resp = client.post("/predictions")
    assert resp.status_code == 500


@uses_predictor("output_file")
def test_output_file(client, match):
    res = client.post("/predictions")
    assert res.status_code == 200
    assert res.json() == match(
        {
            "status": "succeeded",
            "output": "data:application/octet-stream;base64,aGVsbG8=",  # hello
        }
    )


@responses.activate
@uses_predictor("output_file_named")
def test_output_file_to_http(client, match):
    responses.add(
        responses.PUT,
        "http://example.com/upload/foo.txt",
        status=201,
        match=[multipart_matcher({"file": ("foo.txt", b"hello")})],
    )

    res = client.post(
        "/predictions", json={"output_file_prefix": "http://example.com/upload/"}
    )
    assert res.json() == match(
        {
            "status": "succeeded",
            "output": "http://example.com/upload/foo.txt",
        }
    )
    assert res.status_code == 200


@responses.activate
@uses_predictor_with_client_options("output_file_named", upload_url="https://dontuseme")
def test_output_file_to_http_with_upload_url_specified(client, match):
    # Ensure that even when --upload-url is provided on the command line,
    # uploads continue to go to the specified output_file_prefix, for backwards
    # compatibility.
    responses.add(
        responses.PUT,
        "http://example.com/upload/foo.txt",
        status=201,
        match=[multipart_matcher({"file": ("foo.txt", b"hello")})],
    )

    res = client.post(
        "/predictions", json={"output_file_prefix": "http://example.com/upload/"}
    )
    assert res.json() == match(
        {
            "status": "succeeded",
            "output": "http://example.com/upload/foo.txt",
        }
    )
    assert res.status_code == 200


@uses_predictor("output_path_image")
def test_output_path(client):
    res = client.post("/predictions")
    assert res.status_code == 200
    header, b64data = res.json()["output"].split(",", 1)
    # need both image/bmp and image/x-ms-bmp until https://bugs.python.org/issue44211 is fixed
    assert header in ["data:image/bmp;base64", "data:image/x-ms-bmp;base64"]
    assert len(base64.b64decode(b64data)) == 195894


@responses.activate
@uses_predictor("output_path_text")
def test_output_path_to_http(client, match):
    fh = io.BytesIO(b"hello")
    fh.name = "file.txt"
    responses.add(
        responses.PUT,
        "http://example.com/upload/file.txt",
        status=201,
        match=[multipart_matcher({"file": fh})],
    )

    res = client.post(
        "/predictions", json={"output_file_prefix": "http://example.com/upload/"}
    )
    assert res.json() == match(
        {
            "status": "succeeded",
            "output": "http://example.com/upload/file.txt",
        }
    )
    assert res.status_code == 200


@uses_predictor("output_numpy")
def test_json_output_numpy(client, match):
    resp = client.post("/predictions")
    assert resp.status_code == 200
    assert resp.json() == match({"output": 1.0, "status": "succeeded"})


@uses_predictor("output_complex")
def test_complex_output(client, match):
    resp = client.post("/predictions")
    assert resp.json() == match(
        {
            "output": {
                "file": "data:application/octet-stream;base64,aGVsbG8=",
                "text": "hello",
            },
            "status": "succeeded",
        }
    )
    assert resp.status_code == 200


@uses_predictor("output_iterator_complex")
def test_iterator_of_list_of_complex_output(client, match):
    resp = client.post("/predictions")
    assert resp.json() == match(
        {
            "output": [[{"text": "hello"}]],
            "status": "succeeded",
        }
    )
    assert resp.status_code == 200
