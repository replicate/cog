# curio/subprocess.py
#
# A curio-compatible standin for the subprocess module.  Provides
# asynchronous compatible versions of Popen(), check_output(),
# and run() functions.

__all__ = ['run', 'Popen', 'CompletedProcess', 'CalledProcessError',
           'SubprocessError', 'check_output', 'PIPE', 'STDOUT', 'DEVNULL']

# -- Standard Library

import subprocess
import os
import sys

from subprocess import (
    CompletedProcess,
    SubprocessError,
    CalledProcessError,
    PIPE,
    STDOUT,
    DEVNULL,
)

# -- Curio

from .task import spawn
from .time import sleep
from .errors import CancelledError
from .io import FileStream
from . import thread
from .workers import run_in_thread

if sys.platform.startswith('win'):
    from .file import AsyncFile as FileStream

class Popen(object):
    '''
    Curio wrapper around the Popen class from the subprocess module. All of the
    methods from subprocess.Popen should be available, but the associated file
    objects for stdin, stdout, stderr have been replaced by async versions.
    Certain blocking operations (e.g., wait() and communicate()) have been
    replaced by async compatible implementations.   Explicit timeouts
    are not available. Use the timeout_after() function for timeouts.
    '''

    def __init__(self, args, **kwargs):
        if 'universal_newlines' in kwargs:
            raise RuntimeError('universal_newlines argument not supported')

        # If stdin has been given and it's set to a curio FileStream object,
        # then we need to flip it to blocking.
        if 'stdin' in kwargs:
            stdin = kwargs['stdin']
            if isinstance(stdin, FileStream):
                # At hell's heart I stab thy coroutine attempting to read from a stream
                # that's been used as a pipe input to a subprocess.  Must set back to
                # blocking or all hell breaks loose in the child.
                if hasattr(os, 'set_blocking'):
                    os.set_blocking(stdin.fileno(), True)

        self._popen = subprocess.Popen(args, **kwargs)

        if self._popen.stdin:
            self.stdin = FileStream(self._popen.stdin)
        if self._popen.stdout:
            self.stdout = FileStream(self._popen.stdout)
        if self._popen.stderr:
            self.stderr = FileStream(self._popen.stderr)

    def __getattr__(self, name):
        return getattr(self._popen, name)

    async def wait(self):
        retcode = self._popen.poll()
        if retcode is None:
            retcode = await run_in_thread(self._popen.wait)
        return retcode

    async def communicate(self, input=b''):
        '''
        Communicates with a subprocess.  input argument gives data to
        feed to the subprocess stdin stream.  Returns a tuple (stdout, stderr)
        corresponding to the process output.  If cancelled, the resulting
        cancellation exception has stdout_completed and stderr_completed
        attributes attached containing the bytes read so far.
        '''
        stdout_task = await spawn(self.stdout.readall) if self.stdout else None
        stderr_task = await spawn(self.stderr.readall) if self.stderr else None
        try:
            if input:
                await self.stdin.write(input)
                await self.stdin.close()

            stdout = await stdout_task.join() if stdout_task else b''
            stderr = await stderr_task.join() if stderr_task else b''
            return (stdout, stderr)
        except CancelledError as err:
            if stdout_task:
                await stdout_task.cancel()
                err.stdout = stdout_task.exception.bytes_read
            else:
                err.stdout = b''

            if stderr_task:
                await stderr_task.cancel()
                err.stderr = stderr_task.exception.bytes_read
            else:
                err.stderr = b''
            raise

    async def __aenter__(self):
        return self

    async def __aexit__(self, *args):
        if self.stdout:
            await self.stdout.close()

        if self.stderr:
            await self.stderr.close()

        if self.stdin:
            await self.stdin.close()

        # Wait for the process to terminate
        await self.wait()

    def __enter__(self):
        return thread.AWAIT(self.__aenter__())

    def __exit__(self, *args):
        return thread.AWAIT(self.__aexit__(*args))


async def run(args, *, stdin=None, input=None, stdout=None, stderr=None, shell=False, check=False):
    '''
    Curio-compatible version of subprocess.run()
    '''
    if input:
        stdin = subprocess.PIPE
    else:
        stdin = None

    async with Popen(args, stdin=stdin, stdout=stdout, stderr=stderr, shell=shell) as process:
        try:
            stdout, stderr = await process.communicate(input)
        except CancelledError as err:
            process.kill()
            stdout, stderr = await process.communicate()
            # Append the remaining stdout, stderr to the exception
            err.stdout += stdout
            err.stderr += stderr
            raise err
        except:
            process.kill()
            raise

    retcode = process.poll()
    if check and retcode:
        raise CalledProcessError(retcode, process.args,
                                 output=stdout, stderr=stderr)
    return CompletedProcess(process.args, retcode, stdout, stderr)


async def check_output(args, *, stdin=None, stderr=None, shell=False, input=None):
    '''
    Curio compatible version of subprocess.check_output()
    '''
    out = await run(args, stdout=PIPE, stdin=stdin, stderr=stderr, shell=shell,
                    check=True, input=input)
    return out.stdout
