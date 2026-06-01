"""Tests for the emit_metric backwards-compatibility shim."""

import subprocess
import sys


class TestEmitMetric:
    """Tests for the deprecated emit_metric import."""

    def test_import_succeeds(self) -> None:
        """Importing emit_metric should not raise."""
        result = subprocess.run(
            [sys.executable, "-c", "from cog import emit_metric"],
            capture_output=True,
            text=True,
        )
        assert result.returncode == 0, f"Import failed: {result.stderr}"

    def test_attribute_access_succeeds(self) -> None:
        """Accessing cog.emit_metric as a module attribute should not raise."""
        result = subprocess.run(
            [sys.executable, "-c", "import cog; cog.emit_metric"],
            capture_output=True,
            text=True,
        )
        assert result.returncode == 0, f"Attribute access failed: {result.stderr}"

    def test_prints_deprecation_to_stderr(self) -> None:
        """First import should print a deprecation message to stderr."""
        result = subprocess.run(
            [sys.executable, "-c", "from cog import emit_metric"],
            capture_output=True,
            text=True,
        )
        assert "emit_metric() is deprecated" in result.stderr

    def test_message_prints_once(self) -> None:
        """The deprecation message should print only once per process, not on every call."""
        code = "from cog import emit_metric\nfrom cog import emit_metric\n"
        result = subprocess.run(
            [sys.executable, "-c", code],
            capture_output=True,
            text=True,
        )
        assert result.stderr.count("emit_metric() is deprecated") == 1

    def test_callable(self) -> None:
        """emit_metric should be callable and not raise outside a prediction context."""
        result = subprocess.run(
            [
                sys.executable,
                "-c",
                "from cog import emit_metric; emit_metric('output_tokens', 42)",
            ],
            capture_output=True,
            text=True,
        )
        assert result.returncode == 0, f"Call failed: {result.stderr}"

    def test_module_attribute_callable(self) -> None:
        """cog.emit_metric(...) style (used in cog-triton, cog-arctic, etc.) should work."""
        result = subprocess.run(
            [
                sys.executable,
                "-c",
                "import cog; cog.emit_metric('input_token_count', 100)",
            ],
            capture_output=True,
            text=True,
        )
        assert result.returncode == 0, (
            f"Call via module attribute failed: {result.stderr}"
        )

    def test_unknown_attr_still_raises(self) -> None:
        """Adding emit_metric shim should not break AttributeError for unknown attrs."""
        result = subprocess.run(
            [sys.executable, "-c", "import cog; cog.NoSuchAttribute"],
            capture_output=True,
            text=True,
        )
        assert result.returncode != 0
        assert "AttributeError" in result.stderr
