import time
import json
import sys
import uuid
import multiprocessing
import os
import contextlib

import redis


@contextlib.contextmanager
def capture_log(redis_host, redis_port, redis_db, log_queue, stage, prediction_id):
    """
    Send each log line to a redis RPUSH queue in addition to an
    existing output stream.
    """

    # if we don't have a log queue, this is a no-op
    if log_queue is None:
        yield

    # if we have a log queue, tee stdout and stderr to redis via a logging subprocess
    else:
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
                redis_host=redis_host,
                redis_port=redis_port,
                redis_db=redis_db,
                queue=log_queue,
                prediction_id=prediction_id,
                stage=stage,
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


class LogProcess(multiprocessing.Process):
    def __init__(
        self,
        redis_host,
        redis_port,
        redis_db,
        queue,
        prediction_id,
        stage,
        pipe_reader,
        old_out_fd,
        done_token,
    ):
        super(LogProcess, self).__init__()
        self.redis_host = redis_host
        self.redis_port = redis_port
        self.redis_db = redis_db
        self.queue = queue
        self.pipe_reader = pipe_reader
        self.prediction_id = prediction_id
        self.stage = stage
        self.old_out_fd = old_out_fd
        self.done_token = done_token

    def run(self):
        # create a new redis client in the subprocess
        self.redis = redis.Redis(
            host=self.redis_host, port=self.redis_port, db=self.redis_db
        )

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

            # write line to the queue
            self.write_line(line)

        # clean up open files
        self.old_out.close()
        self.pipe_reader.close()

    def write_line(self, line):
        # format the log line as json and push it to the queue
        self.redis.rpush(self.queue, self.log_message(line))
        self.old_out.write(line + "\n")

    def log_message(self, line):
        timestamp_sec = time.time()
        return json.dumps(
            {
                "stage": self.stage,
                "id": self.prediction_id,
                "line": line,
                "timestamp_sec": timestamp_sec,
            }
        )
