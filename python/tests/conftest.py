import functools

import pytest


class ObjectMatcher:
    def __init__(self, name, pattern):
        self.name = name
        self.pattern = pattern

    def __eq__(self, other):
        if not isinstance(other, dict):
            return self.pattern == other
        minimal = {k: other[k] for k in self.pattern.keys() if k in other}
        return self.pattern == minimal

    def __repr__(self):
        return f"{self.name}({repr(self.pattern)})"


@pytest.fixture
def match():
    return functools.partial(ObjectMatcher, "match")
