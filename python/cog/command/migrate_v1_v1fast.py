import ast
import sys
from typing import Any, List, Optional, Type, TypeVar

T = TypeVar("T", covariant=True)


def find(nodes: List[Any], tpe: Type[T], attr: str, name: str) -> Optional[T]:
    for n in nodes:
        if type(n) is tpe and getattr(n, attr) == name:
            return n
    return None


def check(file: str, predictor: str) -> None:
    with open(file, "r") as f:
        content = f.read()
    lines = content.splitlines()
    root = ast.parse(content)

    p = find(root.body, ast.ClassDef, "name", predictor)
    if p is None:
        return
    fn = find(p.body, ast.FunctionDef, "name", "predict")
    if fn is None:
        fn = find(p.body, ast.AsyncFunctionDef, "name", "predict")  # type: ignore
    args_and_defaults = zip(fn.args.args[-len(fn.args.defaults) :], fn.args.defaults)  # type: ignore
    none_defaults = []
    for a, d in args_and_defaults:
        if type(a.annotation) is not ast.Name:
            continue
        if type(d) is not ast.Call or d.func.id != "Input":  # type: ignore
            continue
        v = find(d.keywords, ast.keyword, "arg", "default")
        if v is None or type(v.value) is not ast.Constant:
            continue
        if v.value.value is None:
            pos = f"{file}:{a.lineno}:{a.col_offset}"

            # Add `Optional[]` to type annotation
            # No need to remove `default=None` since `x: Optional[T] = Input(default=None)` is valid
            ta = a.annotation
            line = lines[ta.lineno - 1]
            parts = (
                line[: ta.col_offset],
                line[ta.col_offset : ta.end_col_offset],
                line[ta.end_col_offset :],
            )
            line = f"{parts[0]}Optional[{parts[1]}]{parts[2]}"
            lines[ta.lineno - 1] = line

            none_defaults.append(f"{pos}: {a.arg}: {ta.id}={ast.unparse(d)}")

    if len(none_defaults) > 0:
        print(
            "Default value of None without explicit Optional[T] type hint is ambiguous and deprecated, for example:",
            file=sys.stderr,
        )
        print("-     x: str=Input(default=None)", file=sys.stderr)
        print("+     x: Optional[str]=Input(default=None)", file=sys.stderr)
        print(file=sys.stderr)
        for line in none_defaults:
            print(line, file=sys.stderr)

        # Check for `from typing import Optional`
        imports = find(root.body, ast.ImportFrom, "module", "typing")
        if imports is None or "Optional" not in [n.name for n in imports.names]:
            # Missing import, add it at beginning of file or before first import
            # Skip `#!/usr/bin/env python3` or comments
            lno = 1
            while lines[lno - 1].startswith("#"):
                lno += 1
            for n in root.body:
                if type(n) in {ast.Import, ast.ImportFrom}:
                    lno = n.lineno
                    break
            lines = (
                lines[: lno - 1] + ["from typing import Optional"] + lines[lno - 1 :]
            )
    print("\n".join(lines))


check(sys.argv[1], sys.argv[2])
