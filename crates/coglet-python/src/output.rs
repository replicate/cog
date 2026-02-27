//! Output processing for prediction results.
//!
//! Converts Python prediction output to JSON-serializable format:
//! - Pydantic models -> dict (via model_dump() / dict())
//! - Dataclasses -> dict (via dataclasses.asdict())
//! - Enums -> .value
//! - datetime -> .isoformat()
//! - PathLike -> base64 data URL
//! - IOBase -> base64 data URL
//! - numpy int/float/ndarray -> Python int/float/list
//! - dict/list/set/tuple/generator -> recursive descent
//!
//! This replaces the Python modules cog.json and cog.files.

use pyo3::prelude::*;
use pyo3::types::{PyDict, PyFrozenSet, PyList, PySet, PyString, PyTuple};

/// Process prediction output for JSON serialization.
///
/// Calls make_encodeable() to normalize, then encode_files() to convert any
/// remaining Path/IOBase objects to base64 data URLs.
pub fn process_output<'py>(
    py: Python<'py>,
    output: &Bound<'py, PyAny>,
) -> PyResult<Bound<'py, PyAny>> {
    let encodeable = make_encodeable(py, output)?;
    encode_files(py, &encodeable)
}

/// Process a single output item (for generator outputs).
pub fn process_output_item<'py>(
    py: Python<'py>,
    item: &Bound<'py, PyAny>,
) -> PyResult<Bound<'py, PyAny>> {
    process_output(py, item)
}

/// Normalize a Python object into a JSON-friendly form.
///
/// Handles Pydantic models, dataclasses, enums, datetime, numpy types,
/// and collections. PathLike objects are passed through (handled later
/// by encode_files).
fn make_encodeable<'py>(py: Python<'py>, obj: &Bound<'py, PyAny>) -> PyResult<Bound<'py, PyAny>> {
    // Pydantic v2: model_dump()
    if let Ok(method) = obj.getattr("model_dump")
        && method.is_callable()
    {
        let dumped = method.call0()?;
        return make_encodeable(py, &dumped);
    }

    // Pydantic v1: dict()
    // Skip plain dicts -- they also have .dict but we handle those below
    if !obj.is_instance_of::<PyDict>()
        && let Ok(method) = obj.getattr("dict")
        && method.is_callable()
    {
        let dumped = method.call0()?;
        return make_encodeable(py, &dumped);
    }

    // dataclass instances (not the class itself)
    let dataclasses = py.import("dataclasses")?;
    let is_dataclass = dataclasses.getattr("is_dataclass")?;
    if is_dataclass.call1((obj,))?.is_truthy()?
        && !obj.is_instance(py.get_type::<pyo3::types::PyType>().as_any())?
    {
        let asdict = dataclasses.getattr("asdict")?;
        let d = asdict.call1((obj,))?;
        return make_encodeable(py, &d);
    }

    // dict
    if let Ok(dict) = obj.cast_exact::<PyDict>() {
        let new_dict = PyDict::new(py);
        for (key, value) in dict.iter() {
            new_dict.set_item(&key, make_encodeable(py, &value)?)?;
        }
        return Ok(new_dict.into_any());
    }

    // list, set, frozenset, tuple, generator
    if obj.is_instance_of::<PyList>()
        || obj.is_instance_of::<PySet>()
        || obj.is_instance_of::<PyFrozenSet>()
        || obj.is_instance_of::<PyTuple>()
        || is_generator(py, obj)?
    {
        let iter = obj.try_iter()?;
        let items: Vec<Bound<'py, PyAny>> = iter
            .map(|item| make_encodeable(py, &item?))
            .collect::<PyResult<_>>()?;
        let list = PyList::new(py, &items)?;
        return Ok(list.into_any());
    }

    // Enum -> .value
    let enum_mod = py.import("enum")?;
    let enum_cls = enum_mod.getattr("Enum")?;
    if obj.is_instance(&enum_cls)? {
        return obj.getattr("value");
    }

    // datetime -> .isoformat()
    let datetime_mod = py.import("datetime")?;
    let datetime_cls = datetime_mod.getattr("datetime")?;
    if obj.is_instance(&datetime_cls)? {
        return obj.call_method0("isoformat");
    }

    // os.PathLike -> pathlib.Path (will be encoded to base64 later by encode_files)
    let os_mod = py.import("os")?;
    let pathlike_cls = os_mod.getattr("PathLike")?;
    if obj.is_instance(&pathlike_cls)? {
        let pathlib = py.import("pathlib")?;
        let path_cls = pathlib.getattr("Path")?;
        return path_cls.call1((obj,));
    }

    // numpy types (optional)
    if let Ok(np) = py.import("numpy")
        && !obj.is_instance(py.get_type::<pyo3::types::PyType>().as_any())?
    {
        let np_integer = np.getattr("integer")?;
        if obj.is_instance(&np_integer)? {
            let builtins = py.import("builtins")?;
            return builtins.getattr("int")?.call1((obj,));
        }
        let np_floating = np.getattr("floating")?;
        if obj.is_instance(&np_floating)? {
            let builtins = py.import("builtins")?;
            return builtins.getattr("float")?.call1((obj,));
        }
        let np_ndarray = np.getattr("ndarray")?;
        if obj.is_instance(&np_ndarray)? {
            return obj.call_method0("tolist");
        }
    }

    // Primitive / unknown -- pass through
    Ok(obj.clone())
}

/// Recursively walk the output and encode any Path/IOBase objects to base64 data URLs.
fn encode_files<'py>(py: Python<'py>, obj: &Bound<'py, PyAny>) -> PyResult<Bound<'py, PyAny>> {
    // str -- return as-is (don't recurse into characters)
    if obj.is_instance_of::<PyString>() {
        return Ok(obj.clone());
    }

    // dict
    if let Ok(dict) = obj.cast_exact::<PyDict>() {
        let new_dict = PyDict::new(py);
        for (key, value) in dict.iter() {
            new_dict.set_item(&key, encode_files(py, &value)?)?;
        }
        return Ok(new_dict.into_any());
    }

    // list
    if let Ok(list) = obj.cast_exact::<PyList>() {
        let items: Vec<Bound<'py, PyAny>> = list
            .iter()
            .map(|item| encode_files(py, &item))
            .collect::<PyResult<_>>()?;
        let new_list = PyList::new(py, &items)?;
        return Ok(new_list.into_any());
    }

    // os.PathLike -> open and base64 encode
    let os_mod = py.import("os")?;
    let pathlike_cls = os_mod.getattr("PathLike")?;
    if obj.is_instance(&pathlike_cls)? {
        let builtins = py.import("builtins")?;
        let fh = builtins.getattr("open")?.call1((obj, "rb"))?;
        let result = file_to_base64(py, &fh);
        fh.call_method0("close")?;
        return result;
    }

    // io.IOBase -> base64 encode
    let io_mod = py.import("io")?;
    let iobase_cls = io_mod.getattr("IOBase")?;
    if obj.is_instance(&iobase_cls)? {
        return file_to_base64(py, obj);
    }

    // Primitive -- pass through
    Ok(obj.clone())
}

/// Encode a file handle to a base64 data URL.
///
/// Seeks to start if seekable, reads all bytes, guesses MIME type from
/// the file name, and returns "data:{mime};base64,{encoded}".
fn file_to_base64<'py>(py: Python<'py>, fh: &Bound<'py, PyAny>) -> PyResult<Bound<'py, PyAny>> {
    // Seek to start if possible
    if let Ok(seekable) = fh.call_method0("seekable")
        && seekable.is_truthy()?
    {
        fh.call_method1("seek", (0,))?;
    }

    // Read content
    let content = fh.call_method0("read")?;
    let bytes: Vec<u8> = if content.is_instance_of::<PyString>() {
        let s: String = content.extract()?;
        s.into_bytes()
    } else {
        content.extract()?
    };

    // Guess MIME type from filename
    let mime_type = if let Ok(name) = fh.getattr("name")
        && !name.is_none()
    {
        let name_str: String = name.extract()?;
        let mimetypes = py.import("mimetypes")?;
        let guess = mimetypes.call_method1("guess_type", (&name_str,))?;
        let first = guess.get_item(0)?;
        if first.is_none() {
            "application/octet-stream".to_string()
        } else {
            first.extract()?
        }
    } else {
        "application/octet-stream".to_string()
    };

    // Base64 encode
    use base64::Engine as _;
    let encoded = base64::engine::general_purpose::STANDARD.encode(&bytes);
    let data_url = format!("data:{mime_type};base64,{encoded}");

    Ok(PyString::new(py, &data_url).into_any())
}

/// Check if a Python object is a generator instance.
fn is_generator<'py>(py: Python<'py>, obj: &Bound<'py, PyAny>) -> PyResult<bool> {
    let types_mod = py.import("types")?;
    let gen_type = types_mod.getattr("GeneratorType")?;
    obj.is_instance(&gen_type)
}

#[cfg(test)]
mod tests {
    use super::*;
    use base64::Engine as _;
    use pyo3::types::PyDict;

    /// Helper: evaluate a Python expression and run make_encodeable on it.
    fn encodeable(py_expr: &str) -> String {
        pyo3::Python::initialize();
        Python::attach(|py| {
            let locals = PyDict::new(py);
            // Run any setup + expression, storing result in `obj`
            let code = format!("import json\n{}\nresult = obj", py_expr);
            py.run(&std::ffi::CString::new(code).unwrap(), None, Some(&locals))
                .expect("failed to evaluate test expression");

            let obj = locals.get_item("result").unwrap().unwrap();
            let encoded = make_encodeable(py, &obj).expect("make_encodeable failed");

            // Convert to JSON string for easy assertion
            let json_mod = py.import("json").unwrap();
            let json_str = json_mod
                .call_method1("dumps", (&encoded,))
                .expect("json.dumps failed");
            json_str.extract::<String>().unwrap()
        })
    }

    /// Helper: evaluate a Python expression and run process_output on it.
    fn processed(py_expr: &str) -> String {
        pyo3::Python::initialize();
        Python::attach(|py| {
            let locals = PyDict::new(py);
            let code = format!("import json\n{}\nresult = obj", py_expr);
            py.run(&std::ffi::CString::new(code).unwrap(), None, Some(&locals))
                .expect("failed to evaluate test expression");

            let obj = locals.get_item("result").unwrap().unwrap();
            let output = process_output(py, &obj).expect("process_output failed");

            let json_mod = py.import("json").unwrap();
            let json_str = json_mod
                .call_method1("dumps", (&output,))
                .expect("json.dumps failed");
            json_str.extract::<String>().unwrap()
        })
    }

    // ── make_encodeable: primitives ──────────────────────────────────

    #[test]
    fn encodeable_string() {
        assert_eq!(encodeable("obj = 'hello'"), r#""hello""#);
    }

    #[test]
    fn encodeable_int() {
        assert_eq!(encodeable("obj = 42"), "42");
    }

    #[test]
    fn encodeable_float() {
        assert_eq!(encodeable("obj = 3.14"), "3.14");
    }

    #[test]
    fn encodeable_bool() {
        assert_eq!(encodeable("obj = True"), "true");
    }

    #[test]
    fn encodeable_none() {
        assert_eq!(encodeable("obj = None"), "null");
    }

    // ── make_encodeable: collections ─────────────────────────────────

    #[test]
    fn encodeable_list() {
        assert_eq!(encodeable("obj = [1, 2, 3]"), "[1, 2, 3]");
    }

    #[test]
    fn encodeable_dict() {
        assert_eq!(
            encodeable(r#"obj = {"a": 1, "b": 2}"#),
            r#"{"a": 1, "b": 2}"#
        );
    }

    #[test]
    fn encodeable_tuple_to_list() {
        assert_eq!(encodeable("obj = (1, 2, 3)"), "[1, 2, 3]");
    }

    #[test]
    fn encodeable_set_to_list() {
        // Set with single element to avoid ordering issues
        assert_eq!(encodeable("obj = {42}"), "[42]");
    }

    #[test]
    fn encodeable_frozenset_to_list() {
        assert_eq!(encodeable("obj = frozenset([99])"), "[99]");
    }

    #[test]
    fn encodeable_nested_dict() {
        assert_eq!(
            encodeable(r#"obj = {"outer": {"inner": [1, 2]}}"#),
            r#"{"outer": {"inner": [1, 2]}}"#
        );
    }

    // ── make_encodeable: enum ────────────────────────────────────────

    #[test]
    fn encodeable_enum() {
        assert_eq!(
            encodeable("import enum\nclass Color(enum.Enum):\n    RED = 'red'\nobj = Color.RED"),
            r#""red""#
        );
    }

    #[test]
    fn encodeable_int_enum() {
        assert_eq!(
            encodeable(
                "import enum\nclass Priority(enum.IntEnum):\n    HIGH = 1\nobj = Priority.HIGH"
            ),
            "1"
        );
    }

    // ── make_encodeable: datetime ────────────────────────────────────

    #[test]
    fn encodeable_datetime() {
        let result =
            encodeable("from datetime import datetime\nobj = datetime(2025, 1, 15, 10, 30, 0)");
        assert_eq!(result, r#""2025-01-15T10:30:00""#);
    }

    // ── make_encodeable: dataclass ───────────────────────────────────

    #[test]
    fn encodeable_dataclass() {
        assert_eq!(
            encodeable(
                "from dataclasses import dataclass\n\
                 @dataclass\n\
                 class Point:\n\
                 \tx: int\n\
                 \ty: int\n\
                 obj = Point(x=1, y=2)"
            ),
            r#"{"x": 1, "y": 2}"#
        );
    }

    #[test]
    fn encodeable_nested_dataclass() {
        assert_eq!(
            encodeable(
                "from dataclasses import dataclass, asdict\n\
                 @dataclass\n\
                 class Inner:\n\
                 \tval: str\n\
                 # Build nested via dict so class scoping isn't an issue\n\
                 obj = {'inner': asdict(Inner(val='hello')), 'name': 'test'}"
            ),
            r#"{"inner": {"val": "hello"}, "name": "test"}"#
        );
    }

    // ── make_encodeable: generator ───────────────────────────────────

    #[test]
    fn encodeable_generator() {
        assert_eq!(encodeable("obj = (x * 2 for x in range(3))"), "[0, 2, 4]");
    }

    // ── make_encodeable: enum value in collection ────────────────────

    #[test]
    fn encodeable_enum_in_list() {
        assert_eq!(
            encodeable(
                "import enum\n\
                 class Status(enum.Enum):\n\
                 \tOK = 'ok'\n\
                 \tERR = 'err'\n\
                 obj = [Status.OK, Status.ERR]"
            ),
            r#"["ok", "err"]"#
        );
    }

    // ── encode_files / file_to_base64 ────────────────────────────────

    #[test]
    fn encode_pathlike_to_base64() {
        let result = processed(
            "import tempfile, pathlib\n\
             f = tempfile.NamedTemporaryFile(suffix='.txt', delete=False)\n\
             f.write(b'hello world')\n\
             f.close()\n\
             obj = pathlib.Path(f.name)",
        );
        assert!(
            result.starts_with(r#""data:text/plain;base64,"#),
            "expected data URL, got: {result}"
        );
        // Verify the base64 content decodes correctly
        let b64_part = result.trim_matches('"').split(",").nth(1).unwrap();
        let decoded = base64::engine::general_purpose::STANDARD
            .decode(b64_part)
            .unwrap();
        assert_eq!(decoded, b"hello world");
    }

    #[test]
    fn encode_iobase_to_base64() {
        let result = processed(
            "import io\n\
             obj = io.BytesIO(b'test bytes')",
        );
        assert!(
            result.starts_with(r#""data:application/octet-stream;base64,"#),
            "expected data URL, got: {result}"
        );
        let b64_part = result.trim_matches('"').split(",").nth(1).unwrap();
        let decoded = base64::engine::general_purpose::STANDARD
            .decode(b64_part)
            .unwrap();
        assert_eq!(decoded, b"test bytes");
    }

    #[test]
    fn encode_iobase_seeks_to_start() {
        let result = processed(
            "import io\n\
             buf = io.BytesIO(b'rewind me')\n\
             buf.read()  # advance to end\n\
             obj = buf",
        );
        let b64_part = result.trim_matches('"').split(",").nth(1).unwrap();
        let decoded = base64::engine::general_purpose::STANDARD
            .decode(b64_part)
            .unwrap();
        assert_eq!(decoded, b"rewind me", "should seek to start before reading");
    }

    #[test]
    fn encode_file_in_dict() {
        let result = processed(
            "import io\n\
             obj = {'output': io.BytesIO(b'nested')}",
        );
        // Parse the JSON to verify structure
        let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();
        let url = parsed["output"].as_str().unwrap();
        assert!(
            url.starts_with("data:application/octet-stream;base64,"),
            "expected data URL in dict value"
        );
    }

    #[test]
    fn encode_file_in_list() {
        let result = processed(
            "import io\n\
             obj = [io.BytesIO(b'item1'), io.BytesIO(b'item2')]",
        );
        let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();
        assert!(parsed.as_array().unwrap().len() == 2);
        for item in parsed.as_array().unwrap() {
            assert!(item.as_str().unwrap().starts_with("data:"));
        }
    }

    #[test]
    fn encode_string_passthrough() {
        // Strings should NOT be recursed into
        assert_eq!(processed("obj = 'just a string'"), r#""just a string""#);
    }

    #[test]
    fn encode_mime_type_guessing() {
        let result = processed(
            "import tempfile, pathlib\n\
             f = tempfile.NamedTemporaryFile(suffix='.png', delete=False)\n\
             f.write(b'\\x89PNG')\n\
             f.close()\n\
             obj = pathlib.Path(f.name)",
        );
        assert!(
            result.contains("image/png"),
            "expected image/png MIME type, got: {result}"
        );
    }

    // ── process_output: end-to-end ───────────────────────────────────

    #[test]
    fn process_output_primitives_passthrough() {
        assert_eq!(processed("obj = 'hello'"), r#""hello""#);
        assert_eq!(processed("obj = 42"), "42");
        assert_eq!(processed("obj = None"), "null");
    }

    #[test]
    fn process_output_dataclass_with_file() {
        let result = processed(
            "from dataclasses import dataclass\n\
             import pathlib, tempfile\n\
             @dataclass\n\
             class Output:\n\
             \ttext: str\n\
             \tdata: object\n\
             f = tempfile.NamedTemporaryFile(suffix='.bin', delete=False)\n\
             f.write(b'binary')\n\
             f.close()\n\
             obj = Output(text='result', data=pathlib.Path(f.name))",
        );
        let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();
        assert_eq!(parsed["text"], "result");
        assert!(parsed["data"].as_str().unwrap().starts_with("data:"));
    }
}
