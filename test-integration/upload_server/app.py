import os

import flask
from flask import Flask, jsonify, send_from_directory

app = Flask("upload_server")


@app.route("/", methods=["GET"])
def handle_index():
    return "OK"


@app.route("/upload", methods=["PUT"])
def handle_upload():
    f = flask.request.files["file"]
    f.save(os.path.join("/uploads", f.filename))
    return jsonify({"url": "http://upload-server:5000/download/{}".format(f.filename)})


@app.route("/download/<name>")
def download(name):
    return send_from_directory("/uploads", name)


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000, debug=True)
