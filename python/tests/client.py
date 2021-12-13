from flask.testing import FlaskClient
from cog.server.http import HTTPServer

def make_client(version) -> FlaskClient:
    app = HTTPServer(version).make_app()
    app.config["TESTING"] = True
    with app.test_client() as client:
        return client
