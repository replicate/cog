import json
import os
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Any, List


class SimpleHTTPRequestHandler(BaseHTTPRequestHandler):
    _requests: List[dict[str, Any]] = []

    def do_GET(self):
        self.send_response(200)
        self.send_header('Content-type', 'application/json')
        self.end_headers()
        if self.path == '/_requests':
            self.wfile.write(json.dumps(self._requests).encode('utf-8'))
        else:
            self.save_request('GET')

    def do_POST(self):
        length = int(self.headers['Content-Length'])
        data = self.rfile.read(length).decode('utf-8')
        self.save_request('POST', json.loads(data))
        self.send_response(200)
        self.end_headers()

    def save_request(self, method: str, body: Any = None):
        req = {'method': method, 'path': self.path, 'body': body}
        self._requests.append(req)


if __name__ == '__main__':
    port = int(os.environ['PORT'])
    s = HTTPServer(('', port), SimpleHTTPRequestHandler)
    print(f'Listening on :{port}...')
    s.serve_forever()
