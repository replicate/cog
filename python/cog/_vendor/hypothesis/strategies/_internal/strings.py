# This file is part of Hypothesis, which may be found at
# https://github.com/HypothesisWorks/hypothesis/
#
# Copyright the Hypothesis Authors.
# Individual contributors are listed in AUTHORS.rst and the git log.
#
# This Source Code Form is subject to the terms of the Mozilla Public License,
# v. 2.0. If a copy of the MPL was not distributed with this file, You can
# obtain one at https://mozilla.org/MPL/2.0/.

import copy
import warnings

from cog._vendor.hypothesis.errors import HypothesisWarning, InvalidArgument
from cog._vendor.hypothesis.internal import charmap
from cog._vendor.hypothesis.internal.conjecture.utils import biased_coin, integer_range
from cog._vendor.hypothesis.internal.intervalsets import IntervalSet
from cog._vendor.hypothesis.strategies._internal.collections import ListStrategy
from cog._vendor.hypothesis.strategies._internal.strategies import SearchStrategy


class OneCharStringStrategy(SearchStrategy):
    """A strategy which generates single character strings of text type."""

    def __init__(
        self,
        whitelist_categories=None,
        blacklist_categories=None,
        blacklist_characters=None,
        min_codepoint=None,
        max_codepoint=None,
        whitelist_characters=None,
    ):
        assert set(whitelist_categories or ()).issubset(charmap.categories())
        assert set(blacklist_categories or ()).issubset(charmap.categories())
        intervals = charmap.query(
            include_categories=whitelist_categories,
            exclude_categories=blacklist_categories,
            min_codepoint=min_codepoint,
            max_codepoint=max_codepoint,
            include_characters=whitelist_characters,
            exclude_characters=blacklist_characters,
        )
        self._arg_repr = ", ".join(
            f"{k}={v!r}"
            for k, v in [
                ("whitelist_categories", whitelist_categories),
                ("blacklist_categories", blacklist_categories),
                ("whitelist_characters", whitelist_characters),
                ("blacklist_characters", blacklist_characters),
                ("min_codepoint", min_codepoint),
                ("max_codepoint", max_codepoint),
            ]
            if not (v in (None, "") or (k == "blacklist_categories" and v == ("Cs",)))
        )
        if not intervals:
            raise InvalidArgument(
                "No characters are allowed to be generated by this "
                f"combination of arguments: {self._arg_repr}"
            )
        self.intervals = IntervalSet(intervals)
        self.zero_point = self.intervals.index_above(ord("0"))
        self.Z_point = min(
            self.intervals.index_above(ord("Z")), len(self.intervals) - 1
        )

    def __repr__(self):
        return f"characters({self._arg_repr})"

    def do_draw(self, data):
        if len(self.intervals) > 256:
            if biased_coin(data, 0.2):
                i = integer_range(data, 256, len(self.intervals) - 1)
            else:
                i = integer_range(data, 0, 255)
        else:
            i = integer_range(data, 0, len(self.intervals) - 1)

        i = self.rewrite_integer(i)

        return chr(self.intervals[i])

    def rewrite_integer(self, i):
        # We would like it so that, where possible, shrinking replaces
        # characters with simple ascii characters, so we rejig this
        # bit so that the smallest values are 0, 1, 2, ..., Z.
        #
        # Imagine that numbers are laid out as abc0yyyZ...
        # this rearranges them so that they are laid out as
        # 0yyyZcba..., which gives a better shrinking order.
        if i <= self.Z_point:
            # We want to rewrite the integers [0, n] inclusive
            # to [zero_point, Z_point].
            n = self.Z_point - self.zero_point
            if i <= n:
                i += self.zero_point
            else:
                # We want to rewrite the integers [n + 1, Z_point] to
                # [zero_point, 0] (reversing the order so that codepoints below
                # zero_point shrink upwards).
                i = self.zero_point - (i - n)
                assert i < self.zero_point
            assert 0 <= i <= self.Z_point
        return i


class TextStrategy(ListStrategy):
    def do_draw(self, data):
        return "".join(super().do_draw(data))

    def __repr__(self):
        args = []
        if repr(self.element_strategy) != "characters()":
            args.append(repr(self.element_strategy))
        if self.min_size:
            args.append(f"min_size={self.min_size}")
        if self.max_size < float("inf"):
            args.append(f"max_size={self.max_size}")
        return f"text({', '.join(args)})"

    # See https://docs.python.org/3/library/stdtypes.html#string-methods
    # These methods always return Truthy values for any nonempty string.
    _nonempty_filters = ListStrategy._nonempty_filters + (
        str,
        str.capitalize,
        str.casefold,
        str.encode,
        str.expandtabs,
        str.join,
        str.lower,
        str.rsplit,
        str.split,
        str.splitlines,
        str.swapcase,
        str.title,
        str.upper,
    )
    _nonempty_and_content_filters = (
        str.isidentifier,
        str.islower,
        str.isupper,
        str.isalnum,
        str.isalpha,
        str.isascii,
        str.isdecimal,
        str.isdigit,
        str.isnumeric,
        str.isspace,
        str.istitle,
        str.lstrip,
        str.rstrip,
        str.strip,
    )

    def filter(self, condition):
        if condition in (str.lower, str.title, str.upper):
            warnings.warn(
                f"You applied str.{condition.__name__} as a filter, but this allows "
                f"all nonempty strings!  Did you mean str.is{condition.__name__}?",
                HypothesisWarning,
            )
        # We use ListStrategy filter logic for the conditions that *only* imply
        # the string is nonempty.  Here, we increment the min_size but still apply
        # the filter for conditions that imply nonempty *and specific contents*.
        if condition in self._nonempty_and_content_filters:
            assert self.max_size >= 1, "Always-empty is special cased in st.text()"
            self = copy.copy(self)
            self.min_size = max(1, self.min_size)
            return ListStrategy.filter(self, condition)

        return super().filter(condition)


class FixedSizeBytes(SearchStrategy):
    def __init__(self, size):
        self.size = size

    def do_draw(self, data):
        return bytes(data.draw_bytes(self.size))
