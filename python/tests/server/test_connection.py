import multiprocessing as mp

import pytest
from cog.server import eventtypes
from cog.server.connection import AsyncConnection


@pytest.mark.asyncio
async def test_async_connection_rt():
    item = ("asdf", eventtypes.PredictionOutput({"x": 3}))
    c1, c2 = mp.Pipe()
    ac = AsyncConnection(c1)
    await ac.async_init()
    ac.send(item)
    # we expect the binary format to be compatible
    assert c2.recv() == item
    c2.send(item)
    assert await ac.recv() == item
