import sys
import uuid
import multiprocessing
import os
import contextlib


@contextlib.contextmanager
def capture_log(logs_dest):
    """
    Send each line from stdout and stderr to a pipe in addition to the existing
    output stream.
    """

    outs = {
        "stdout": sys.stdout,
        "stderr": sys.stderr,
    }
    pipe_readers = {}
    pipe_writers = {}
    old_fds = {}
    procs = {}

    # done_token is sent to the logging subprocess as a sentinel signal to break
    # its infinite loop
    done_token = str(uuid.uuid4())

    for out_name, out_file in outs.items():

        # create a pipe that we'll send to the logging subprocess
        pipe_readers[out_name], pipe_writers[out_name] = multiprocessing.Pipe(
            duplex=False
        )

        # get a handle to the original stdout/stderr file descriptor
        old_fds[out_name] = os.dup(out_file.fileno())

        # redirect stdout/stderr to the pipe
        os.dup2(pipe_writers[out_name].fileno(), out_file.fileno())

        # start the logging subprocess as a daemon
        procs[out_name] = LogProcess(
            logs_dest=logs_dest,
            pipe_reader=pipe_readers[out_name],
            old_out_fd=old_fds[out_name],
            done_token=done_token,
        )
        procs[out_name].daemon = True
        procs[out_name].start()

    try:
        # do the work
        yield

    finally:

        # write done token to exit the logging loop
        for out_file in outs.values():
            out_file.write(done_token + "\n")

        for out_name, out_file in outs.items():

            # exit the logging process as gracefully as possible
            procs[out_name].join(timeout=1.0)
            procs[out_name].close()

            # clean up pipe
            pipe_readers[out_name].close()
            pipe_writers[out_name].close()

            # reset stdout/stderr to original file descriptor
            os.dup2(old_fds[out_name], out_file.fileno())

            # close the reference to the original file descriptor
            os.close(old_fds[out_name])


# `multiprocessing.get_context("fork")` returns the same API as
# `multiprocessing`, but will use the fork method when creating any subprocess.
# Although fork is the default, we need this here because the log process gets
# created from the predictor subprocess, which is set to use the spawn method.
# As currently written, this log process relies on shared state in a way that
# doesn't work with the spawn method.
class LogProcess(multiprocessing.get_context("fork").Process):
    def __init__(
        self,
        logs_dest,
        pipe_reader,
        old_out_fd,
        done_token,
    ):
        super(LogProcess, self).__init__()
        self.logs_dest = logs_dest
        self.pipe_reader = pipe_reader
        self.old_out_fd = old_out_fd
        self.done_token = done_token

    def run(self):
        # open the original stdout/stderr. we "tee" to both the original
        # stdout/stderr as well as the redis queue
        self.old_out = os.fdopen(self.old_out_fd, "w")

        # open pipe reader
        read_file = os.fdopen(self.pipe_reader.fileno(), "r", 1)

        # infinite loop until we get the done token
        while True:

            # read a line from the pipe, stripping the newline
            line = read_file.readline().rstrip()

            # did we get the done token?
            if line.strip() == self.done_token:
                break

            # send the logs to the old stream and the logs pipe
            self.old_out.write(line + "\n")
            self.logs_dest.send(line)

        # clean up open files
        self.old_out.close()
        self.pipe_reader.close()
