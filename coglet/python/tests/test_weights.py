import os.path

import pytest

from coglet import inspector, runner


@pytest.mark.asyncio
async def test_weights_none():
    p = inspector.create_predictor('tests.schemas.weights', 'Predictor')
    r = runner.Runner(p)
    await r.setup()
    assert await r.predict({'i': 0}) == ''


@pytest.mark.asyncio
async def test_weights_url():
    p = inspector.create_predictor('tests.schemas.weights', 'Predictor')
    r = runner.Runner(p)
    os.environ['COG_WEIGHTS'] = 'http://r8.im/weights.tar'
    await r.setup()
    assert await r.predict({'i': 0}) == 'http://r8.im/weights.tar'
    del os.environ['COG_WEIGHTS']


@pytest.mark.asyncio
async def test_weights_path(tmp_path):
    p = inspector.create_predictor('tests.schemas.weights', 'Predictor')
    r = runner.Runner(p)
    os.chdir(tmp_path)
    os.mkdir(os.path.join(tmp_path, 'weights'))
    await r.setup()
    assert await r.predict({'i': 0}) == 'weights'
