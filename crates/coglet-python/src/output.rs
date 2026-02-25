//! Output processing for prediction results.
//!
//! Converts Python prediction output to JSON-serializable format:
//! - Pydantic models → dict (via model_dump() / dict())
//! - Dataclasses → dict (via dataclasses.asdict())
//! - Enums → .value
//! - datetime → .isoformat()
//! - PathLike → base64 data URL
//! - IOBase → base64 data URL
//! - numpy int/float/ndarray → Python int/float/list
//! - dict/list/set/tuple/generator → recursive descent
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
fn make_encodeable<'py>(
    py: Python<'py>,
    obj: &Bound<'py, PyAny>,
) -> PyResult<Bound<'py, PyAny>> {
    // Pydantic v2: model_dump()
    if let Ok(method) = obj.getattr("model_dump")
        && method.is_callable()
    {
        let dumped = method.call0()?;
        return make_encodeable(py, &dumped);
    }

    // Pydantic v1: dict()
    // Skip plain dicts — they also have .dict but we handle those below
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

    // Enum → .value
    let enum_mod = py.import("enum")?;
    let enum_cls = enum_mod.getattr("Enum")?;
    if obj.is_instance(&enum_cls)? {
        return obj.getattr("value");
    }

    // datetime → .isoformat()
    let datetime_mod = py.import("datetime")?;
    let datetime_cls = datetime_mod.getattr("datetime")?;
    if obj.is_instance(&datetime_cls)? {
        return obj.call_method0("isoformat");
    }

    // os.PathLike → pathlib.Path (will be encoded to base64 later by encode_files)
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

    // Primitive / unknown — pass through
    Ok(obj.clone())
}

/// Recursively walk the output and encode any Path/IOBase objects to base64 data URLs.
fn encode_files<'py>(
    py: Python<'py>,
    obj: &Bound<'py, PyAny>,
) -> PyResult<Bound<'py, PyAny>> {
    // str — return as-is (don't recurse into characters)
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

    // os.PathLike → open and base64 encode
    let os_mod = py.import("os")?;
    let pathlike_cls = os_mod.getattr("PathLike")?;
    if obj.is_instance(&pathlike_cls)? {
        let builtins = py.import("builtins")?;
        let fh = builtins.getattr("open")?.call1((obj, "rb"))?;
        let result = file_to_base64(py, &fh);
        fh.call_method0("close")?;
        return result;
    }

    // io.IOBase → base64 encode
    let io_mod = py.import("io")?;
    let iobase_cls = io_mod.getattr("IOBase")?;
    if obj.is_instance(&iobase_cls)? {
        return file_to_base64(py, obj);
    }

    // Primitive — pass through
    Ok(obj.clone())
}

/// Encode a file handle to a base64 data URL.
///
/// Seeks to start if seekable, reads all bytes, guesses MIME type from
/// the file name, and returns "data:{mime};base64,{encoded}".
fn file_to_base64<'py>(
    py: Python<'py>,
    fh: &Bound<'py, PyAny>,
) -> PyResult<Bound<'py, PyAny>> {
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
