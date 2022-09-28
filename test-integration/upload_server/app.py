import os

import flask
from flask import Flask, redirect, request, send_from_directory

app = Flask("upload_server")


@app.route("/", methods=["GET"])
def handle_index():
    return "OK"


@app.route("/upload/<name>", methods=["PUT"])
def redirect_upload(name):
    print("I'm in here!", name)
    return redirect(f"/files/{name}", 307)


@app.route("/files/<name>", methods=["PUT"])
def upload_file(name):
    with open(os.path.join("/uploads", name), "wb") as f:
        f.write(request.get_data())
    return "OK"


@app.route("/files/<name>", methods=["GET"])
def download_file(name):
    return send_from_directory("/uploads", name)


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000, debug=True)
