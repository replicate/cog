import json
import os.path
from dataclasses import MISSING, Field
from typing import Any, Dict

from coglet import adt


def to_json_input(predictor: adt.Predictor) -> Dict[str, Any]:
    in_schema: Dict[str, Any] = {
        'properties': {},
        'type': 'object',
        'title': 'Input',
    }
    required = []
    for name, adt_in in predictor.inputs.items():
        prop: Dict[str, Any] = {
            'x-order': adt_in.order,
        }
        if adt_in.choices is not None:
            prop['allOf'] = [{'$ref': f'#/components/schemas/{name}'}]
        else:
            prop['title'] = name.replace('_', ' ').title()
            prop.update(adt_in.type.json_type())

        # With <name>: <type> = Input(default=None)
        # Legacy Cog does not include <name> in "required" fields or set "default" value
        # This allows None to be passed to `str` or `List[str]` which is incorrect

        # <name>:<type> = Input() - "required"
        # <name>:<type> = Input(default=<value>) - not "required", has "default"
        # <name>:Optional[<type>] = Input() - not "required", default to None
        # <name>:Optional[<type>] = Input(default=<value>) - not "required", has "default"
        # <name>:list[<type>] = Input() - "required"
        # <name>:list[<type>] = Input(default=[<value>]) - not "required", has "default"
        if adt_in.default is None:
            if adt_in.type.repetition in {
                adt.Repetition.REQUIRED,
                adt.Repetition.REPEATED,
            }:
                required.append(name)
        else:
            # Handle dataclass fields by extracting the actual default value
            if isinstance(adt_in.default, Field):
                if adt_in.default.default_factory is not MISSING:
                    # For default_factory, get a sample value for schema
                    actual_default = adt_in.default.default_factory()
                elif adt_in.default.default is not MISSING:
                    actual_default = adt_in.default.default
                else:
                    actual_default = None
            else:
                actual_default = adt_in.default

            if actual_default is not None:
                # First normalize the value to convert raw types to proper objects (e.g., str -> Secret)
                normalized_default = adt_in.type.normalize(actual_default)
                prop['default'] = adt_in.type.json_encode(normalized_default)

        # <name>: Optional[<type>] implies nullable, regardless of default
        if adt_in.type.repetition is adt.Repetition.OPTIONAL:
            prop['nullable'] = True

        if adt_in.description is not None:
            prop['description'] = adt_in.description
        if adt_in.ge is not None:
            prop['minimum'] = adt_in.ge
        if adt_in.le is not None:
            prop['maximum'] = adt_in.le
        if adt_in.min_length is not None:
            prop['minLength'] = adt_in.min_length
        if adt_in.max_length is not None:
            prop['maxLength'] = adt_in.max_length
        if adt_in.regex is not None:
            prop['pattern'] = adt_in.regex
        if adt_in.deprecated is not None:
            prop['deprecated'] = adt_in.deprecated
        in_schema['properties'][name] = prop
    if len(required) > 0:
        in_schema['required'] = required
    return in_schema


def to_json_enums(predictor: adt.Predictor) -> Dict[str, Any]:
    enums = {}
    for name, adt_in in predictor.inputs.items():
        if adt_in.choices is None:
            continue
        t = {
            'title': name,
            'description': 'An enumeration.',
            'enum': adt_in.choices,
        }
        t.update(adt_in.type.primitive.json_type())
        enums[name] = t
    return enums


def to_json_output(predictor: adt.Predictor) -> Dict[str, Any]:
    return predictor.output.json_type()


def to_json_schema(predictor: adt.Predictor) -> Dict[str, Any]:
    path = os.path.join(os.path.dirname(__file__), 'openapi.json')
    with open(path, 'r') as f:
        schema = json.load(f)
    schema['components']['schemas']['Input'] = to_json_input(predictor)
    schema['components']['schemas']['Output'] = to_json_output(predictor)
    schema['components']['schemas'].update(to_json_enums(predictor))
    return schema
