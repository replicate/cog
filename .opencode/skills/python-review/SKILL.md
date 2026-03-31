---
name: python-review
description: Python code review guidelines for the Cog SDK
---

## Python review guidelines

This project uses Python for the SDK (`python/cog/`) which defines the predictor
interface, type system, and HTTP/queue server.

### What linters already catch (skip these)

ruff handles pycodestyle (E), Pyflakes (F), isort (I), warnings (W), bandit (S),
bugbear (B), and annotations (ANN). Don't flag issues these would catch.

### What to look for

**Type annotations**
- Required on all function signatures
- Use `typing_extensions` for backward compatibility
- Avoid `Any` where a concrete type is possible
- Check that type annotations actually match runtime behavior

**Compatibility**
- Must support Python 3.10 through 3.13
- Watch for syntax/stdlib features only available in newer versions
- `from __future__ import annotations` if using newer annotation syntax

**Error handling**
- No bare `except:` or `except Exception:` that swallows everything
- Exceptions should have descriptive messages
- Resource cleanup with context managers, not try/finally when avoidable

**Async patterns**
- Tests use pytest-asyncio -- async tests need proper fixtures
- Watch for blocking calls inside async functions
- Proper cleanup of async resources (aclose, async context managers)

**Predictor interface**
- `base_predictor.py` is the core interface -- changes here affect all users
- `types.py` defines input/output types -- check backward compatibility
- Server code in `python/cog/server/` handles HTTP -- watch for request handling bugs

**Testing**
- Uses pytest with fixtures
- tox runs tests across Python 3.10-3.13
- Test isolation: don't rely on global state or test ordering
