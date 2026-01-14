import subprocess
from typing import Any

import pexpect
import pytest


@pytest.mark.skipif(
    pexpect is None,
    reason="pexpect not available; install it in integration env",
)
def test_run_shell_with_with_interactive_tty(
    tmpdir_factory: Any, cog_binary: str
) -> None:
    tmpdir = tmpdir_factory.mktemp("project")
    (tmpdir / "cog.yaml").write_text(
        "build:\n  python_version: '3.13'\n  cog_runtime: true\n",
        encoding="utf-8",
    )

    child = pexpect.spawn(
        str(cog_binary) + " run /bin/bash",
        cwd=str(tmpdir),
        encoding="utf-8",
        timeout=20,
    )
    child.expect(r"#")  # wait for bash prompt
    child.sendline("echo OK")
    child.expect("OK")
    child.sendline("exit")
    child.close()
