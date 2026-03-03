"""Tests for the ExperimentalFeatureWarning backwards-compatibility shim."""

import subprocess
import sys


class TestExperimentalFeatureWarning:
    """Tests for the deprecated ExperimentalFeatureWarning import."""

    def test_import_succeeds(self) -> None:
        """Importing ExperimentalFeatureWarning should not raise."""
        result = subprocess.run(
            [sys.executable, "-c", "from cog import ExperimentalFeatureWarning"],
            capture_output=True,
            text=True,
        )
        assert result.returncode == 0, f"Import failed: {result.stderr}"

    def test_prints_deprecation_to_stderr(self) -> None:
        """First import should print a deprecation message to stderr."""
        result = subprocess.run(
            [sys.executable, "-c", "from cog import ExperimentalFeatureWarning"],
            capture_output=True,
            text=True,
        )
        assert "ExperimentalFeatureWarning is deprecated" in result.stderr

    def test_message_prints_once(self) -> None:
        """The deprecation message should print only once per process."""
        code = (
            "from cog import ExperimentalFeatureWarning\n"
            "from cog import ExperimentalFeatureWarning\n"
        )
        result = subprocess.run(
            [sys.executable, "-c", code],
            capture_output=True,
            text=True,
        )
        assert result.stderr.count("ExperimentalFeatureWarning is deprecated") == 1

    def test_is_future_warning_subclass(self) -> None:
        """The shim class should be a subclass of FutureWarning."""
        code = (
            "from cog import ExperimentalFeatureWarning\n"
            "assert issubclass(ExperimentalFeatureWarning, FutureWarning)\n"
        )
        result = subprocess.run(
            [sys.executable, "-c", code],
            capture_output=True,
            text=True,
        )
        assert result.returncode == 0, f"Assertion failed: {result.stderr}"

    def test_filterwarnings_compat(self) -> None:
        """The real use case: warnings.filterwarnings('ignore', ...) should work."""
        code = (
            "import warnings\n"
            "from cog import ExperimentalFeatureWarning\n"
            "warnings.filterwarnings('ignore', category=ExperimentalFeatureWarning)\n"
        )
        result = subprocess.run(
            [sys.executable, "-c", code],
            capture_output=True,
            text=True,
        )
        assert result.returncode == 0, f"filterwarnings failed: {result.stderr}"

    def test_unknown_attr_raises(self) -> None:
        """Accessing a non-existent attribute should raise AttributeError."""
        code = "import cog\ncog.NoSuchAttribute\n"
        result = subprocess.run(
            [sys.executable, "-c", code],
            capture_output=True,
            text=True,
        )
        assert result.returncode != 0
        assert "AttributeError" in result.stderr
