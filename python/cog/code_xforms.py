import ast
import re
import types
from typing import Optional, Set, Union

COG_IMPORT_MODULES = {"cog", "typing", "sys", "os", "functools", "pydantic", "numpy"}


def load_module_from_string(
    name: str, source: Optional[str]
) -> Optional[types.ModuleType]:
    if not source or not name:
        return None
    module = types.ModuleType(name)
    exec(source, module.__dict__)  # noqa: S102
    return module


def extract_class_source(source_code: str, class_name: str) -> str:
    """
    Extracts the source code for a specified class from a given source text.
    Args:
        source_code: The complete source code as a string.
        class_name: The name of the class to extract.
    Returns:
        The source code of the specified class if found; otherwise, an empty string.
    """
    class_name_pattern = re.compile(r"\b[A-Z]\w*\b")
    all_class_names = class_name_pattern.findall(class_name)

    class ClassExtractor(ast.NodeVisitor):
        def __init__(self) -> None:
            self.class_source = None

        def visit_ClassDef(self, node: ast.ClassDef) -> None:
            if node.name in all_class_names:
                self.class_source = ast.get_source_segment(source_code, node)

    tree = ast.parse(source_code)
    extractor = ClassExtractor()
    extractor.visit(tree)
    return extractor.class_source if extractor.class_source else ""


def extract_function_source(source_code: str, function_name: str) -> str:
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
            self.function_source = None

        def visit_FunctionDef(self, node: ast.FunctionDef) -> None:
            if node.name == function_name and not isinstance(node, ast.Module):
                # Extract the source segment for this function definition
                self.function_source = ast.get_source_segment(source_code, node)

    tree = ast.parse(source_code)
    extractor = FunctionExtractor()
    extractor.visit(tree)
    return extractor.function_source if extractor.function_source else ""


def make_class_methods_empty(source_code: Union[str, ast.AST], class_name: str) -> str:
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
        def visit_ClassDef(self, node: ast.ClassDef) -> Optional[ast.AST]:
            if node.name == class_name:
                for body_item in node.body:
                    if isinstance(body_item, ast.FunctionDef):
                        # Replace the body of the method with `return None`
                        body_item.body = [ast.Return(value=ast.Constant(value=None))]
                return node

    tree = source_code if isinstance(source_code, ast.AST) else ast.parse(source_code)
    transformer = MethodBodyTransformer()
    transformed_tree = transformer.visit(tree)
    class_code = ast.unparse(transformed_tree)
    return class_code


def extract_method_return_type(
    source_code: Union[str, ast.AST], class_name: str, method_name: str
) -> Optional[str]:
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
            self.return_type = None

        def visit_ClassDef(self, node: ast.ClassDef) -> None:
            if node.name == class_name:
                self.generic_visit(node)

        def visit_FunctionDef(self, node: ast.FunctionDef) -> None:
            if node.name == method_name and node.returns:
                self.return_type = ast.unparse(node.returns)

    tree = source_code if isinstance(source_code, ast.AST) else ast.parse(source_code)
    extractor = MethodReturnTypeExtractor()
    extractor.visit(tree)

    return extractor.return_type


def extract_function_return_type(
    source_code: Union[str, ast.AST], function_name: str
) -> Optional[str]:
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
            self.return_type = None

        def visit_FunctionDef(self, node: ast.FunctionDef) -> None:
            if node.name == function_name and node.returns:
                # Extract and return the string representation of the return type
                self.return_type = ast.unparse(node.returns)

    tree = source_code if isinstance(source_code, ast.AST) else ast.parse(source_code)
    extractor = FunctionReturnTypeExtractor()
    extractor.visit(tree)

    return extractor.return_type


def make_function_empty(source_code: Union[str, ast.AST], function_name: str) -> str:
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
        def visit_FunctionDef(self, node: ast.FunctionDef) -> Optional[ast.AST]:
            if node.name == function_name:
                # Replace the body of the function with `return None`
                node.body = [ast.Return(value=ast.Constant(value=None))]
                return node

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

        def visit_Import(self, node: ast.Import) -> None:
            for alias in node.names:
                if alias.name in module_names:
                    self.imports.append(ast.unparse(node))

        def visit_ImportFrom(self, node: ast.ImportFrom) -> None:
            if node.module in module_names:
                self.imports.append(ast.unparse(node))

    tree = source_code if isinstance(source_code, ast.AST) else ast.parse(source_code)
    extractor = ImportExtractor()
    extractor.visit(tree)

    return "\n".join(extractor.imports)


def strip_model_source_code(
    source_code: str, class_name: str, method_name: str
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
    class_source = (
        None if not class_name else extract_class_source(source_code, class_name)
    )
    if class_source:
        class_source = make_class_methods_empty(class_source, class_name)
        return_type = extract_method_return_type(class_source, class_name, method_name)
        return_class_source = (
            extract_class_source(source_code, return_type) if return_type else ""
        )
        model_source = (
            imports + "\n\n" + return_class_source + "\n\n" + class_source + "\n"
        )
    else:
        # use class_name specified in cog.yaml as method_name
        method_name = class_name
        function_source = extract_function_source(source_code, method_name)
        if not function_source:
            return None
        function_source = make_function_empty(function_source, method_name)
        if not function_source:
            return None
        return_type = extract_function_return_type(function_source, method_name)
        return_class_source = (
            extract_class_source(source_code, return_type) if return_type else ""
        )
        model_source = (
            imports + "\n\n" + return_class_source + "\n\n" + function_source + "\n"
        )
    return model_source
