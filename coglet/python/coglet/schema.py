import json
import os.path
import sys

from coglet import inspector, schemas


def main():
    if len(sys.argv) != 3:
        print(f'Usage {os.path.basename(sys.argv[0])} <MODULE> <CLASS>')
        sys.exit(1)

    # Some libraries print progress upon import and mess up schema JSON
    _buffer = ''

    def _write(s: str) -> int:
        nonlocal _buffer
        _buffer += s
        return len(s)

    _stdout_write = sys.stdout.write
    _stderr_write = sys.stderr.write
    sys.stdout.write = _write
    sys.stderr.write = _write

    try:
        # This could fail due to various reasons:
        # - Bad dependencies
        # - Bad input/output types
        # - Libraries downloading weights on init
        p = inspector.create_predictor(sys.argv[1], sys.argv[2])

        # Skipping these for now as old models have no test inputs and will fail schema validation
        # Check that test_inputs exists and is valid
        # module = importlib.import_module(p.module_name)
        # cls = getattr(module, p.predictor_name)
        # inspector.get_test_inputs(cls, p.inputs)

        schema = schemas.to_json_schema(p)
    except Exception as e:
        sys.stdout.write = _stdout_write
        sys.stderr.write = _stderr_write
        print(_buffer)
        raise e
    finally:
        sys.stdout.write = _stdout_write
        sys.stderr.write = _stderr_write

    print(json.dumps(schema, indent=2))


if __name__ == '__main__':
    main()
