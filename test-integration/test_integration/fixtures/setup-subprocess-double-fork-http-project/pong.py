import os
import signal
import time
from random import randint
from wsgiref.simple_server import make_server


def main():
    child_pid = os.fork()
    is_child = child_pid == 0

    pid = os.getpid()

    if is_child:
        make_server("127.0.0.1", 7777, app).serve_forever()
    else:
        while True:
            print(f"===> PARENT ({pid})")

            time.sleep(10)


def app(environ, start_response):
    print(f"---> CHILD ({os.getpid()})")

    if environ["PATH_INFO"] == "/ping":
        start_response("200 OK", [("content-type", "text/plain")])
        return [b"PONG\n" for n in range(100 + randint(2, 32))]

    start_response("404 Not Found", [("content-type", "text/plain")])
    return [b"NO\n"]


if __name__ == "__main__":
    main()
