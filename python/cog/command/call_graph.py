"""A CLI for determining the call graph of a pipeline."""

import ast
import sys
from pathlib import Path
from typing import List


class IncludeAnalyzer(ast.NodeVisitor):
    def __init__(self, file_path: Path) -> None:
        self.file_path = file_path
        self.includes: List[str] = []
        self.imports: dict[str, str] = {}
        self.scope_stack: List[str] = []

    def visit_Import(self, node: ast.Import) -> None:
        for alias in node.names:
            self.imports[alias.asname or alias.name] = alias.name
        self.generic_visit(node)

    def visit_ImportFrom(self, node: ast.ImportFrom) -> None:
        module = node.module or ""
        for alias in node.names:
            full_name = f"{module}.{alias.name}" if module else alias.name
            self.imports[alias.asname or alias.name] = full_name
        self.generic_visit(node)

    # Scope tracking
    def visit_FunctionDef(self, node: ast.FunctionDef) -> None:
        self.scope_stack.append("function")
        self.generic_visit(node)
        self.scope_stack.pop()

    def visit_AsyncFunctionDef(self, node: ast.AsyncFunctionDef) -> None:
        self.scope_stack.append("function")
        self.generic_visit(node)
        self.scope_stack.pop()

    def visit_ClassDef(self, node: ast.ClassDef) -> None:
        self.scope_stack.append("class")
        self.generic_visit(node)
        self.scope_stack.pop()

    def visit_Lambda(self, node: ast.Lambda) -> None:
        self.scope_stack.append("lambda")
        self.generic_visit(node)
        self.scope_stack.pop()

    def visit_Call(self, node: ast.Call) -> None:
        target = None

        if isinstance(node.func, ast.Attribute):
            # Handles replicate.include
            if isinstance(node.func.value, ast.Name):
                target = f"{self.imports.get(node.func.value.id, node.func.value.id)}.{node.func.attr}"
        elif isinstance(node.func, ast.Name):
            # Handles `from replicate import include` then `include(...)`
            target = self.imports.get(node.func.id, node.func.id)

        if target == "replicate.use":
            # Check scope
            if self.scope_stack:
                raise ValueError(
                    f"[{self.file_path}] Invalid scope at line {node.lineno}: `replicate.use(...)` must be in global scope"
                )
            elif node.args:
                arg = node.args[0]
                if isinstance(arg, ast.Constant) and isinstance(arg.value, str):
                    self.includes.append(arg.s)
                else:
                    raise ValueError(
                        f"[{self.file_path}] Unresolvable argument at line {node.lineno}: Not a string literal"
                    )
        self.generic_visit(node)


def analyze_python_file(
    file_path: Path,
) -> List[str]:
    source = file_path.read_text()
    tree = ast.parse(source, filename=str(file_path))
    analyzer = IncludeAnalyzer(file_path)
    analyzer.visit(tree)
    return analyzer.includes


def main(filepath: str) -> None:
    """Run the main code for determining the call graph of a pipeline."""
    includes = analyze_python_file(Path(filepath))
    print(",".join(sorted(list(set(includes)))))


if __name__ == "__main__":
    main(sys.argv[1])
