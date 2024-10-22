import ast
import re
import types
from typing import List, Optional, Set, Tuple, Union

COG_IMPORT_MODULES = {
    "cog",
    "typing",
    "typing_extensions",
    "sys",
    "os",
    "functools",
    "pydantic",
    "numpy",
}


def load_module_from_string(
    name: str, source: Optional[str]
) -> Optional[types.ModuleType]:
    if not source or not name:
        return None
    module = types.ModuleType(name)
    exec(source, module.__dict__)  # noqa: S102 # pylint: disable=exec-used
    return module


def extract_class_sources(source_code: str, class_names: List[str]) -> List[str]:
    """
    Extracts the source code for a specified class from a given source text.
    Args:
        source_code: The complete source code as a string.
        class_name: The name of the class to extract.
    Returns:
        The source code of the specified class if found; otherwise, an empty string.
    """
    class_name_pattern = re.compile(r"\b[A-Z]\w*\b")
    all_class_names = []
    for class_name in class_names:
        all_class_names.extend(class_name_pattern.findall(class_name))

    class ClassExtractor(ast.NodeVisitor):
        def __init__(self) -> None:
            self.class_sources = []

        def visit_ClassDef(self, node: ast.ClassDef) -> None:  # pylint: disable=invalid-name
            self.class_sources.append(node)

    tree = ast.parse(source_code)
    extractor = ClassExtractor()
    extractor.visit(tree)

    valid_class_names = set(all_class_names)
    for node in extractor.class_sources:
        if node.name not in valid_class_names:
            continue
        for base_name in node.bases:
            valid_class_names.add(base_name.id)

    return [
        str(ast.get_source_segment(source_code, x))
        for x in extractor.class_sources
        if x.name in valid_class_names
    ]


def extract_function_source(source_code: str, function_names: List[str]) -> str:
    """
    Extracts the source code for a specified function from a given source text.
    Args:
        source_code: The complete source code as a string.
        function_name: The name of the function to extract.
    Returns:
        The source code of the specified function if found; otherwise, an empty string.
    """

    class FunctionExtractor(ast.NodeVisitor):
        def __init__(self) -> None:
            self.function_sources = []

        def visit_FunctionDef(self, node: ast.FunctionDef) -> None:  # pylint: disable=invalid-name
            if node.name in function_names and not isinstance(node, ast.Module):
                # Extract the source segment for this function definition
                self.function_sources.append(ast.get_source_segment(source_code, node))

    tree = ast.parse(source_code)
    extractor = FunctionExtractor()
    extractor.visit(tree)
    return "\n".join(extractor.function_sources)


def make_class_methods_empty(
    source_code: Union[str, ast.AST],
    class_name: Optional[str],
    global_vars: List[ast.Assign],
) -> Tuple[str, List[ast.Assign]]:
    """
    Transforms the source code of a specified class to remove the bodies of all its methods
    and replace them with 'return None'.
    Args:
        source_code: The complete source code as a string.
        class_name: The name of the class to transform.
    Returns:
        The transformed source code of the specified class.
    """

    class MethodBodyTransformer(ast.NodeTransformer):
        def __init__(self, global_vars: List[ast.Assign]) -> None:
            self.used_globals = set()
            self._targets = {
                target.id: global_name
                for global_name in global_vars
                for target in global_name.targets
                if isinstance(target, ast.Name)
            }

        def visit_ClassDef(self, node: ast.ClassDef) -> Optional[ast.AST]:  # pylint: disable=invalid-name
            if class_name is None or node.name == class_name:
                for body_item in node.body:
                    if isinstance(body_item, ast.FunctionDef):
                        # Replace the body of the method with `return None`
                        body_item.body = [ast.Return(value=ast.Constant(value=None))]
                        # Remove decorators from the function
                        body_item.decorator_list = []
                        # Determine if one our globals is referenced by the function.
                        for default in body_item.args.defaults:
                            if isinstance(default, ast.Call):
                                for keyword in default.keywords:
                                    if isinstance(keyword.value, ast.Name):
                                        corresponding_global = self._targets.get(
                                            keyword.value.id
                                        )
                                        if corresponding_global is not None:
                                            self.used_globals.add(corresponding_global)
                return node

            return None

    tree = source_code if isinstance(source_code, ast.AST) else ast.parse(source_code)
    transformer = MethodBodyTransformer(global_vars)
    transformed_tree = transformer.visit(tree)
    class_code = ast.unparse(transformed_tree)
    return class_code, list(transformer.used_globals)


def extract_method_return_type(
    source_code: Union[str, ast.AST], class_names: List[str], method_names: List[str]
) -> List[str]:
    """
    Extracts the return type annotation of a specified method within a given class from the source code.
    Args:
        source_code: A string containing the Python source code.
        class_name: The name of the class containing the method of interest.
        method_name: The name of the method whose return type annotation is to be extracted.
    Returns:
        A string representing the method's return type annotation if found; otherwise, None.
    """

    class MethodReturnTypeExtractor(ast.NodeVisitor):
        def __init__(self) -> None:
            self.return_types = []

        def visit_ClassDef(self, node: ast.ClassDef) -> None:  # pylint: disable=invalid-name
            if node.name in class_names:
                self.generic_visit(node)

        def visit_FunctionDef(self, node: ast.FunctionDef) -> None:  # pylint: disable=invalid-name
            if node.name in method_names and node.returns:
                self.return_types.append(ast.unparse(node.returns))

    tree = source_code if isinstance(source_code, ast.AST) else ast.parse(source_code)
    extractor = MethodReturnTypeExtractor()
    extractor.visit(tree)

    return extractor.return_types


def extract_function_return_types(
    source_code: Union[str, ast.AST], function_names: List[str]
) -> List[str]:
    """
    Extracts the return type annotation of a specified function from the source code.
    Args:
        source_code: A string containing the Python source code.
        function_name: The name of the function whose return type annotation is to be extracted.
    Returns:
        A string representing the function's return type annotation if found; otherwise, None.
    """

    class FunctionReturnTypeExtractor(ast.NodeVisitor):
        def __init__(self) -> None:
            self.return_types = []

        def visit_FunctionDef(self, node: ast.FunctionDef) -> None:  # pylint: disable=invalid-name
            if node.name in function_names and node.returns:
                # Extract and return the string representation of the return type
                self.return_types.append(ast.unparse(node.returns))

    tree = source_code if isinstance(source_code, ast.AST) else ast.parse(source_code)
    extractor = FunctionReturnTypeExtractor()
    extractor.visit(tree)

    return extractor.return_types


def make_function_empty(
    source_code: Union[str, ast.AST], function_names: List[str]
) -> str:
    """
    Transforms the source code to remove the body of a specified function
    and replace it with 'return None'.
    Args:
        source_code: The complete source code as a string or an AST node.
        function_name: The name of the function to transform.
    Returns:
        The transformed source code with the specified function's body emptied.
    """

    class FunctionBodyTransformer(ast.NodeTransformer):
        def visit_FunctionDef(self, node: ast.FunctionDef) -> Optional[ast.AST]:  # pylint: disable=invalid-name
            if node.name in function_names:
                # Replace the body of the function with `return None`
                node.body = [ast.Return(value=ast.Constant(value=None))]
                return node

            return None

    tree = source_code if isinstance(source_code, ast.AST) else ast.parse(source_code)
    transformer = FunctionBodyTransformer()
    transformed_tree = transformer.visit(tree)
    modified_code = ast.unparse(transformed_tree)
    return modified_code


def extract_specific_imports(
    source_code: Union[str, ast.AST], module_names: Set[str]
) -> str:
    """
    Extracts import statements from the source code that match a specified list of module names.
    Args:
        source_code: The Python source code as a string.
        module_names: A set of module names for which to extract import statements.
    Returns:
        A list of strings, each string is an import statement that matches one of the specified module names.
    """

    class ImportExtractor(ast.NodeVisitor):
        def __init__(self) -> None:
            self.imports = []

        def visit_Import(self, node: ast.Import) -> None:  # pylint: disable=invalid-name
            for alias in node.names:
                if alias.name in module_names:
                    self.imports.append(ast.unparse(node))

        def visit_ImportFrom(self, node: ast.ImportFrom) -> None:  # pylint: disable=invalid-name
            if node.module in module_names:
                self.imports.append(ast.unparse(node))

    tree = source_code if isinstance(source_code, ast.AST) else ast.parse(source_code)
    extractor = ImportExtractor()
    extractor.visit(tree)

    return "\n".join(extractor.imports)


def _extract_globals(source_code: Union[str, ast.AST]) -> List[ast.Assign]:
    tree = source_code if isinstance(source_code, ast.AST) else ast.parse(source_code)
    if isinstance(tree, ast.Module):
        return [x for x in tree.body if isinstance(x, ast.Assign)]
    return []


def _render_globals(global_vars: List[ast.Assign]) -> str:
    return "\n".join([ast.unparse(x) for x in global_vars])


def strip_model_source_code(
    source_code: str, class_names: List[str], method_names: List[str]
) -> Optional[str]:
    """
    Strips down the model source code by extracting relevant classes and making methods empty.
    Args:
        source_code: The complete model source code as a string.
        class_name: The name of the class to be processed. If empty or the class is not found,
                    it falls back to processing a function specified by `method_name`.
        method_name: The name of the method (if processing a class) or the function (if processing standalone functions)
                     whose return type is to be extracted and used in generating the final model source.
    Returns:
        A string containing the modified source code, including a predefined header.
        Returns None if neither the class nor the function specified could be found or processed.
    """
    imports = extract_specific_imports(source_code, COG_IMPORT_MODULES)
    class_sources = (
        None if not class_names else extract_class_sources(source_code, class_names)
    )
    global_vars = _extract_globals(source_code)
    if class_sources:
        class_source = "\n".join(class_sources)
        class_source, global_vars = make_class_methods_empty(
            class_source, None, global_vars
        )
        return_types = extract_method_return_type(
            class_source, class_names, method_names
        )
        return_class_sources = (
            extract_class_sources(source_code, return_types) if return_types else ""
        )
        return_class_source = "\n".join(return_class_sources)
        rendered_globals = _render_globals(global_vars)
        model_source = "\n".join(
            [
                x
                for x in [imports, rendered_globals, return_class_source, class_source]
                if x
            ]
        )
    else:
        # use class_name specified in cog.yaml as method_name
        method_names = class_names
        function_source = extract_function_source(source_code, method_names)
        if not function_source:
            return None
        function_source = make_function_empty(function_source, method_names)
        if not function_source:
            return None
        return_types = extract_function_return_types(function_source, method_names)
        return_class_sources = (
            extract_class_sources(source_code, return_types) if return_types else ""
        )
        return_class_source = "\n".join(return_class_sources)
        model_source = "\n".join([imports, return_class_source, function_source])
    return model_source
