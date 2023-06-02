import ast
from cog._vendor import curio
import cog._vendor.curio.monitor as monitor
import code
import inspect
import sys
import types
import warnings
import threading
import signal
import os

assert (sys.version_info.major >= 3 and sys.version_info.minor >= 8), "console requires Python 3.8+"

class CurioIOInteractiveConsole(code.InteractiveConsole):

    def __init__(self, locals):
        super().__init__(locals)
        self.compile.compiler.flags |= ast.PyCF_ALLOW_TOP_LEVEL_AWAIT
        self.requests = curio.UniversalQueue()
        self.response = curio.UniversalQueue()

    def runcode(self, code):
        # This coroutine is handed from the thread running the REPL to the
        # task runner in the main thread.
        async def run_it():
            func = types.FunctionType(code, self.locals)
            try:
                # We restore the default REPL signal handler for running normal code
                hand = signal.signal(signal.SIGINT, signal.default_int_handler)
                try:
                    coro = func()
                finally:
                    signal.signal(signal.SIGINT, hand)
            except BaseException as ex:
                await self.response.put((None, ex))
                return
            if not inspect.iscoroutine(coro):
                await self.response.put((coro, None))
                return

            # For a coroutine... We're going to try and do some magic to intercept
            # Control-C in an Event/Task.
            async def watch_ctrl_c(evt, repl_task):
                await evt.wait()
                await repl_task.cancel()
            evt = curio.UniversalEvent()
            try:
                hand = signal.signal(signal.SIGINT, lambda signo, frame: evt.set())
                repl_task = await curio.spawn(coro)
                watch_task = await curio.spawn(watch_ctrl_c, evt, repl_task)
                try:
                    result = await repl_task.join()
                    response = (result, None)
                except SystemExit:
                    raise
                except BaseException as e:
                    await repl_task.wait()
                    response = (None, e.__cause__)
                await watch_task.cancel()
            finally:
                signal.signal(signal.SIGINT, hand)
            await self.response.put(response)

        self.requests.put(run_it())
        # Get the result here...
        result, exc = self.response.get()
        if exc is not None:
            try:
                raise exc
            except BaseException:
                self.showtraceback()
        else:
            return result

    # Task that runs in the main thread, executing input fed to it from above
    async def runmain(self):
        try:
            hand = signal.signal(signal.SIGINT, signal.SIG_IGN)
            while True:
                coro = await self.requests.get()
                if coro is None:
                    break
                await coro
        finally:
            signal.signal(signal.SIGINT, hand)

def run_repl(console):
    try:
        banner = (
            f'curio REPL {sys.version} on {sys.platform}\n'
            f'Use "await" directly instead of "curio.run()".\n'
            f'Type "help", "copyright", "credits" or "license" '
            f'for more information.\n'
            f'{getattr(sys, "ps1", ">>> ")}import curio'
            )
        console.interact(
            banner=banner,
            exitmsg='exiting curio REPL...')
    finally:
        warnings.filterwarnings(
            'ignore',
            message=r'^coroutine .* was never awaited$',
            category=RuntimeWarning)
        console.requests.put(None)

if __name__ == '__main__':
    repl_locals = { 'curio': curio,
                    'ps': monitor.ps,
                    'where': monitor.where,
    }
    for key in {'__name__', '__package__',
                '__loader__', '__spec__',
                '__builtins__', '__file__'}:
        repl_locals[key] = locals()[key]

    console = CurioIOInteractiveConsole(repl_locals)
    threading.Thread(target=run_repl, args=[console], daemon=True).start()
    curio.run(console.runmain)
