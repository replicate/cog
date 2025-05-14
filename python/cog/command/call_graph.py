"""A CLI for determining the call graph of a pipeline."""

import ast
import sys
from pathlib import Path
from typing import List


class IncludeAnalyzer(ast.NodeVisitor):
    def __init__(self, file_path: Path) -> None:
        self.file_path = file_path
        self.includes: List[str] = []
        self.errors: List[str] = []
        self.imports: dict[str, str] = {}

    def visit_Import(self, node: ast.Import):
        for alias in node.names:
            self.imports[alias.asname or alias.name] = alias.name
        self.generic_visit(node)

    def visit_ImportFrom(self, node: ast.ImportFrom):
        module = node.module or ""
        for alias in node.names:
            full_name = f"{module}.{alias.name}" if module else alias.name
            self.imports[alias.asname or alias.name] = full_name
        self.generic_visit(node)

    def visit_Call(self, node: ast.Call):
        target = None

        if isinstance(node.func, ast.Attribute):
            if isinstance(node.func.value, ast.Name):
                target = f"{self.imports.get(node.func.value.id, node.func.value.id)}.{node.func.attr}"
        elif isinstance(node.func, ast.Name):
            target = self.imports.get(node.func.id, node.func.id)

        if target == "cog.ext.pipelines.include":
            if node.args:
                arg = node.args[0]
                if isinstance(arg, ast.Str):
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
    print(",".join(includes))


if __name__ == "__main__":
    main(sys.argv[1])
