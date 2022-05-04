import os
import sys
from contextlib import contextmanager
from typing import Iterator


@contextmanager
def suppress_output() -> Iterator[None]:
    null_out = open(os.devnull, "w")
    null_err = open(os.devnull, "w")
    out_fd = sys.stdout.fileno()
    err_fd = sys.stderr.fileno()
    out_dup_fd = os.dup(out_fd)
    err_dup_fd = os.dup(err_fd)
    os.dup2(null_out.fileno(), out_fd)
    os.dup2(null_err.fileno(), err_fd)

    try:
        yield
    finally:
        os.dup2(out_dup_fd, out_fd)
        os.dup2(err_dup_fd, err_fd)
        null_out.close()
        null_err.close()
        os.close(out_dup_fd)
        os.close(err_dup_fd)
