import os
import tempfile

import cog
import pytest
from cog.server.clients import ClientManager


@pytest.mark.asyncio
async def test_upload_files():
    client_manager = ClientManager()
    temp_dir = tempfile.mkdtemp()
    temp_path = os.path.join(temp_dir, "my_file.txt")
    with open(temp_path, "w") as fh:
        fh.write("file content")
    obj = {"path": cog.Path(temp_path)}
    result = await client_manager.upload_files(obj, None)
    assert result == {"path": "data:text/plain;base64,ZmlsZSBjb250ZW50"}
