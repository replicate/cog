import ast
from typing import Callable, List


def format_errs(
    file: str, lines: List[str], lineno: int, col_offset: int, msg: str, help_msg: str
) -> str:
    start = max(0, lineno - 2)
    end = min(len(lines), lineno + 2)
    w = len(str(end))
    errs = [f'{file}:{lineno}:{col_offset}: {msg}']
    for i in range(start, end):
        errs.append(f'%-{w}d | %s' % (i, lines[i]))
        if i == lineno:
            errs.append('%s | %s^' % (' ' * w, ' ' * col_offset))
    errs.append('%s = help: %s' % (' ' * w, help_msg))
    return '\n'.join(errs)


def visit(
    root: ast.AST,
    file: str,
    name: str,
    lines: List[str],
    f: Callable[[ast.AST, str, str, List[str]], List[str]],
) -> List[str]:
    errs = []
    for node in ast.iter_child_nodes(root):
        errs += f(node, file, name, lines)
        errs += visit(node, file, name, lines, f)
    return errs


def inspect_optional(
    node: ast.AST, file: str, name: str, lines: List[str]
) -> List[str]:
    if type(node) is not ast.FunctionDef:
        return []

    if node.name != name:
        return []

    errs = []
    n = len(node.args.defaults)
    for a, d in zip(node.args.args[-n:], node.args.defaults):
        if a.annotation is None:
            continue
        # <name>: <type> = Input(...)
        if (
            type(d) is not ast.Call
            or type(d.func) is not ast.Name
            or d.func.id != 'Input'
        ):
            continue
        # <name>: <type> = Input(default=..., ...)
        kws = [kw for kw in d.keywords if kw.arg == 'default']
        if len(kws) != 1:
            continue
        if type(kws[0].value) is not ast.Constant or kws[0].value.value is not None:
            continue

        if type(a.annotation) is ast.Name:
            # <name>: <type>
            msg = 'input with default=None must be Optional'
            help_msg = f'Change input type to `{a.arg}: Optional[{ast.unparse(a.annotation)}]` instead'
        elif (
            type(a.annotation) is ast.Subscript
            and type(a.annotation.value) is ast.Name
            and a.annotation.value.id in {'list', 'List'}
        ):
            # <name>: list[<type>]
            msg = 'list input should use default=[] instead of default=None'
            help_msg = 'Change `default=None` to `default=[]` instead'
        else:
            # unknown types, shouldn't happen?
            continue

        errs.append(
            format_errs(
                file,
                lines,
                kws[0].lineno,
                kws[0].col_offset,
                msg,
                help_msg,
            )
        )
    return errs


def inspect(file: str, method: str):
    with open(file, 'r') as f:
        content = f.read()

    root = ast.parse(content)
    # line numbers are 1-indexed
    lines = [''] + content.splitlines()

    errs = visit(root, file, method, lines, inspect_optional)
    if len(errs) > 0:
        errs = ['error-prone usage of default=None'] + errs
        raise AssertionError('\n\n'.join(errs))
