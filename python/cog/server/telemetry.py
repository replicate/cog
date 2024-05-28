import os

import requests
from opentelemetry.instrumentation.requests import RequestsInstrumentor
from opentelemetry.sdk.trace import TracerProvider


def requests_session() -> requests.Session:
    """
    Creates a :code:`requests.Session` with opentelementry tracing
    configured so that tracing context is propagated to downstream
    services.
    """
    # NOTE: We just ensure that outgoing requests propagate tracing
    # information, currently we don't perform any tracing or collection
    # within cog itself.
    if "OTEL_SERVICE_NAME" in os.environ:
        tp = TracerProvider()

        # RequestsInstrumentor patches the global `requests.Session` object
        # which includes the global `requests.get` etc. as well as any child
        # sessions created.
        #
        # As we only want specific sessions to be instrumented we need to do
        # some juggling to get things to work, namely we capture the patched
        # `send()` method created as part of `instrument()` and then remove
        # the patch from the Session, before applying it again to the newly
        # created session instance.
        RequestsInstrumentor().instrument(tracer_provider=tp)

        session = requests.Session()
        instrumented_send = session.send

        # Unpatch requests.Session
        RequestsInstrumentor().uninstrument()

        session.send = instrumented_send
    else:
        session = requests.Session()

    return session
