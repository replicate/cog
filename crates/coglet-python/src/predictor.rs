//! Python predictor loading and invocation.

use std::sync::{Arc, OnceLock};

use pyo3::exceptions::{PyIOError, PyValueError};
use pyo3::prelude::*;
use pyo3::types::{PyAny, PyDict, PyTuple};

use coglet_core::worker::SlotSender;
use coglet_core::{PredictionError, PredictionOutput, PredictionResult};

use crate::cancel;
use crate::input::{self, PreparedInput};
use crate::output;

// =============================================================================
// Async helper functions — defined as Python strings, initialized once.
//
// These must be Python `async def` functions to participate in asyncio's event
// loop. They cannot be expressed as pure Rust because they use Python's async
// iteration protocol and ContextVar.set() before awaiting a coroutine.
// =============================================================================

/// Collects an async generator into a list. Initialized once, reused per-call.
static COLLECT_ASYNC_GEN: OnceLock<Py<PyAny>> = OnceLock::new();

/// Sets a ContextVar then awaits a coroutine. Initialized once, reused per-call.
static CTX_WRAPPER: OnceLock<Py<PyAny>> = OnceLock::new();

/// Get or initialize the `_collect_async_gen` Python helper.
fn get_collect_async_gen(py: Python<'_>) -> Result<Py<PyAny>, PredictionError> {
    if let Some(f) = COLLECT_ASYNC_GEN.get() {
        return Ok(f.clone_ref(py));
    }

    let code = c"\
async def _collect_async_gen(agen):
    results = []
    async for item in agen:
        results.append(item)
    return results
";
    let globals = PyDict::new(py);
    py.run(code, Some(&globals), None).map_err(|e| {
        PredictionError::Failed(format!("Failed to define _collect_async_gen: {e}"))
    })?;
    let f = globals
        .get_item("_collect_async_gen")
        .map_err(|e| PredictionError::Failed(format!("Failed to get _collect_async_gen: {e}")))?
        .ok_or_else(|| PredictionError::Failed("_collect_async_gen not found".to_string()))?
        .unbind();
    let _ = COLLECT_ASYNC_GEN.set(f.clone_ref(py));
    Ok(f)
}

/// Get or initialize the `_ctx_wrapper` Python helper.
fn get_ctx_wrapper(py: Python<'_>) -> Result<Py<PyAny>, PredictionError> {
    if let Some(f) = CTX_WRAPPER.get() {
        return Ok(f.clone_ref(py));
    }

    let code = c"\
async def _ctx_wrapper(coro, prediction_id, log_contextvar, scope, scope_contextvar):
    log_contextvar.set(prediction_id)
    scope_contextvar.set(scope)
    return await coro
";
    let globals = PyDict::new(py);
    py.run(code, Some(&globals), None)
        .map_err(|e| PredictionError::Failed(format!("Failed to define _ctx_wrapper: {e}")))?;
    let f = globals
        .get_item("_ctx_wrapper")
        .map_err(|e| PredictionError::Failed(format!("Failed to get _ctx_wrapper: {e}")))?
        .ok_or_else(|| PredictionError::Failed("_ctx_wrapper not found".to_string()))?
        .unbind();
    let _ = CTX_WRAPPER.set(f.clone_ref(py));
    Ok(f)
}

/// Check if a PyErr is a CancelationException or asyncio.CancelledError.
fn is_cancelation_exception(py: Python<'_>, err: &PyErr) -> bool {
    // Check for our static CancelationException type
    if err.is_instance_of::<cancel::CancelationException>(py) {
        return true;
    }

    // Check for asyncio.CancelledError
    if let Ok(asyncio) = py.import("asyncio")
        && let Ok(cancelled_error) = asyncio.getattr("CancelledError")
        && err.is_instance(py, &cancelled_error)
    {
        return true;
    }

    false
}

/// Format a Python validation error.
///
/// Cog validation errors are already formatted as "field: message".
fn format_validation_error(py: Python<'_>, err: &PyErr) -> String {
    err.value(py).to_string()
}

/// Wrap a coroutine with log + metric context and submit it to a shared event loop.
///
/// Sets up ContextVars for both log routing and metric scope recording in the
/// event loop thread (which is different from the worker thread that created the scope).
/// Returns a `concurrent.futures.Future` that resolves when the coroutine completes.
fn submit_async_coroutine(
    py: Python<'_>,
    coro: &Bound<'_, PyAny>,
    event_loop: &Py<PyAny>,
    prediction_id: &str,
    scope: Option<&Py<crate::metric_scope::Scope>>,
) -> Result<Py<PyAny>, PredictionError> {
    let asyncio = py
        .import("asyncio")
        .map_err(|e| PredictionError::Failed(format!("Failed to import asyncio: {}", e)))?;
    let ctx_wrapper = get_ctx_wrapper(py)?;

    // Get the ContextVar instances for log and metric routing
    let log_contextvar = crate::log_writer::get_prediction_contextvar(py)
        .map_err(|e| PredictionError::Failed(format!("Failed to get log ContextVar: {e}")))?;
    let scope_contextvar = crate::metric_scope::get_scope_contextvar_for_async(py)
        .map_err(|e| PredictionError::Failed(format!("Failed to get scope ContextVar: {e}")))?;

    // Resolve the scope object (or create a noop scope if none provided)
    let scope_obj: Py<crate::metric_scope::Scope> = match scope {
        Some(s) => s.clone_ref(py),
        None => Py::new(
            py,
            crate::metric_scope::Scope::noop(py).map_err(|e| {
                PredictionError::Failed(format!("Failed to create noop scope: {}", e))
            })?,
        )
        .map_err(|e| PredictionError::Failed(format!("Failed to wrap noop scope: {}", e)))?,
    };

    // Wrap the coroutine with context setup
    let wrapped_coro = ctx_wrapper
        .call1(
            py,
            (
                coro,
                prediction_id,
                log_contextvar.bind(py),
                scope_obj.bind(py),
                scope_contextvar.bind(py),
            ),
        )
        .map_err(|e| {
            PredictionError::Failed(format!("Failed to wrap coroutine with context: {}", e))
        })?;

    // Submit wrapped coroutine to shared event loop via run_coroutine_threadsafe
    let future = asyncio
        .call_method1(
            "run_coroutine_threadsafe",
            (wrapped_coro.bind(py), event_loop.bind(py)),
        )
        .map_err(|e| PredictionError::Failed(format!("Failed to submit coroutine: {}", e)))?;

    Ok(future.unbind())
}

/// Send a single output item over IPC, routing file outputs to disk.
///
/// For Path outputs (os.PathLike): sends the existing file path via send_file_output.
/// For IOBase outputs: reads bytes, writes to output_dir via write_file_output.
/// For everything else: processes through make_encodeable + upload_files, then send_output.
fn send_output_item(
    py: Python<'_>,
    item: &Bound<'_, PyAny>,
    json_module: &Bound<'_, PyAny>,
    slot_sender: &SlotSender,
) -> Result<(), PredictionError> {
    let os = py
        .import("os")
        .map_err(|e| PredictionError::Failed(format!("Failed to import os: {}", e)))?;
    let io_mod = py
        .import("io")
        .map_err(|e| PredictionError::Failed(format!("Failed to import io: {}", e)))?;
    let pathlike = os
        .getattr("PathLike")
        .map_err(|e| PredictionError::Failed(format!("Failed to get os.PathLike: {}", e)))?;
    let iobase = io_mod
        .getattr("IOBase")
        .map_err(|e| PredictionError::Failed(format!("Failed to get io.IOBase: {}", e)))?;

    if item.is_instance(&pathlike).unwrap_or(false) {
        // Path output — file already on disk, send path reference
        let path_str: String = item
            .call_method0("__fspath__")
            .and_then(|p| p.extract())
            .map_err(|e| PredictionError::Failed(format!("Failed to get fspath: {}", e)))?;
        slot_sender
            .send_file_output(std::path::PathBuf::from(path_str), None)
            .map_err(|e| PredictionError::Failed(format!("Failed to send file output: {}", e)))?;
        return Ok(());
    }

    if item.is_instance(&iobase).unwrap_or(false) {
        // IOBase output — read bytes, write to disk via SlotSender
        // Seek to start if seekable
        if item
            .call_method0("seekable")
            .and_then(|r| r.extract::<bool>())
            .unwrap_or(false)
        {
            let _ = item.call_method1("seek", (0,));
        }
        let data: Vec<u8> = item
            .call_method0("read")
            .and_then(|d| d.extract())
            .map_err(|e| PredictionError::Failed(format!("Failed to read IOBase: {}", e)))?;

        // Try to guess extension from filename
        let ext = item
            .getattr("name")
            .and_then(|n| n.extract::<String>())
            .ok()
            .and_then(|name| {
                std::path::Path::new(&name)
                    .extension()
                    .and_then(|e| e.to_str())
                    .map(|s| s.to_string())
            })
            .unwrap_or_else(|| "bin".to_string());

        slot_sender
            .write_file_output(&data, &ext, None)
            .map_err(|e| PredictionError::Failed(format!("Failed to write file output: {}", e)))?;
        return Ok(());
    }

    // Non-file output - process normally
    let processed = output::process_output_item(py, item)
        .map_err(|e| PredictionError::Failed(format!("Failed to process output item: {}", e)))?;

    let item_str: String = json_module
        .call_method1("dumps", (&processed,))
        .map_err(|e| PredictionError::Failed(format!("Failed to serialize output item: {}", e)))?
        .extract()
        .map_err(|e| PredictionError::Failed(format!("Failed to extract output string: {}", e)))?;

    let item_json: serde_json::Value = serde_json::from_str(&item_str)
        .map_err(|e| PredictionError::Failed(format!("Failed to parse output JSON: {}", e)))?;

    slot_sender
        .send_output(item_json)
        .map_err(|e| PredictionError::Failed(format!("Failed to send output: {}", e)))?;

    Ok(())
}

/// Type alias for Python object (Py<PyAny>).
type PyObject = Py<PyAny>;

/// How a run()/predict() method executes
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PredictKind {
    /// Synchronous function: def run/predict(self, **input) -> Output
    Sync,
    /// Async coroutine: async def run/predict(self, **input) -> Output
    Async,
    /// Async generator: async def run/predict(self, **input) -> AsyncIterator[Output]
    AsyncGen,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PredictMethodName {
    Run,
    Predict,
}

impl PredictMethodName {
    fn as_str(self) -> &'static str {
        match self {
            Self::Run => "run",
            Self::Predict => "predict",
        }
    }
}

/// Whether and how train() exists
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TrainKind {
    /// No train() method
    None,
    /// Synchronous: def train(self, **input) -> Output
    Sync,
    /// Async: async def train(self, **input) -> Output
    Async,
}

/// The predictor's structure and invocation target
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PredictorKind {
    /// Class instance with selected run()/predict() method, optionally train()
    Class {
        method_name: PredictMethodName,
        predict: PredictKind,
        train: TrainKind,
    },
    /// Standalone function (e.g., train.py:train)
    /// The PredictKind describes how the function executes (sync/async/async_gen)
    StandaloneFunction(PredictKind),
}

/// A loaded Python predictor instance.
///
/// Input coercion (URL->Path/File) and FieldInfo default unwrapping are handled
/// in Rust. The Python `_adt` and `_inspector` modules are no longer called.
pub struct PythonPredictor {
    instance: PyObject,
    /// The predictor's kind (class or standalone function) and method execution types
    kind: PredictorKind,
    /// Whether the setup() method is an async def
    setup_is_async: bool,
    concurrent_max: Option<usize>,
}

// PyObject is Send in PyO3 0.23+
// Safety: We only access the instance through Python::attach()
unsafe impl Send for PythonPredictor {}
unsafe impl Sync for PythonPredictor {}

impl PythonPredictor {
    /// Load a predictor from a reference like "predict.py:Predictor".
    pub fn load(py: Python<'_>, predictor_ref: &str) -> PyResult<Self> {
        // Import the cog.predictor module to use its loading function
        let cog_predictor = py.import("cog.predictor")?;
        let load_fn = cog_predictor.getattr("load_predictor_from_ref")?;

        // Load the predictor class and instantiate it
        let instance: PyObject = load_fn.call1((predictor_ref,))?.unbind();

        // Check if this is a standalone function (train mode) or a Predictor instance
        let inspect = py.import("inspect")?;
        let is_function: bool = inspect
            .call_method1("isfunction", (instance.bind(py),))?
            .extract()?;

        let kind = if is_function {
            // Standalone function - detect its async nature
            let (is_async, is_async_gen) = Self::detect_async(py, &instance, "")?;
            let predict_kind = if is_async_gen {
                tracing::info!("Detected async generator train()");
                PredictKind::AsyncGen
            } else if is_async {
                tracing::info!("Detected async train()");
                PredictKind::Async
            } else {
                tracing::info!("Detected sync train()");
                PredictKind::Sync
            };
            PredictorKind::StandaloneFunction(predict_kind)
        } else {
            // Class instance - detect run()/predict() and train() methods
            let method_name = Self::selected_predict_method_name(py, &instance)?;
            let method_name_str = method_name.as_str();
            let (is_async, is_async_gen) = Self::detect_async(py, &instance, method_name_str)?;
            let predict_kind = if is_async_gen {
                tracing::info!("Detected async generator {}()", method_name_str);
                PredictKind::AsyncGen
            } else if is_async {
                tracing::info!("Detected async {}()", method_name_str);
                PredictKind::Async
            } else {
                tracing::info!("Detected sync {}()", method_name_str);
                PredictKind::Sync
            };

            // Check if train() method exists and if it's async
            let train_kind = if instance.bind(py).hasattr("train")? {
                let (train_async, _) = Self::detect_async(py, &instance, "train")?;
                if train_async {
                    tracing::info!("Detected async train()");
                    TrainKind::Async
                } else {
                    tracing::info!("Detected sync train()");
                    TrainKind::Sync
                }
            } else {
                TrainKind::None
            };

            PredictorKind::Class {
                method_name,
                predict: predict_kind,
                train: train_kind,
            }
        };

        // Detect if setup() is async
        let setup_is_async = if !is_function && instance.bind(py).hasattr("setup")? {
            let (is_async, _) = Self::detect_async(py, &instance, "setup")?;
            if is_async {
                tracing::info!("Detected async setup()");
            }
            is_async
        } else {
            false
        };

        let concurrent_max = Self::read_concurrent_max(py, &instance, &kind)?;

        let predictor = Self {
            instance,
            kind,
            setup_is_async,
            concurrent_max,
        };

        tracing::debug!(concurrent_max = ?predictor.concurrent_max(), "Loaded predictor concurrency metadata");

        // Patch FieldInfo defaults on predict/train methods so Python uses actual
        // default values instead of FieldInfo wrapper objects for missing inputs.
        // Input(default=42, description="...") creates a FieldInfo; without patching,
        // Python would pass the FieldInfo itself as the default value.
        if is_function {
            Self::unwrap_field_info_defaults(py, &predictor.instance, "")?;
        } else {
            if let PredictorKind::Class { method_name, .. } = &predictor.kind {
                Self::unwrap_field_info_defaults(py, &predictor.instance, method_name.as_str())?;
            }
            if matches!(predictor.kind, PredictorKind::Class { train, .. } if train != TrainKind::None)
            {
                Self::unwrap_field_info_defaults(py, &predictor.instance, "train")?;
            }
        }

        Ok(predictor)
    }

    fn read_concurrent_max(
        py: Python<'_>,
        instance: &PyObject,
        kind: &PredictorKind,
    ) -> PyResult<Option<usize>> {
        let func = match kind {
            PredictorKind::Class { method_name, .. } => {
                instance.bind(py).getattr(method_name.as_str())?
            }
            PredictorKind::StandaloneFunction(_) => instance.bind(py).clone(),
        };
        Self::extract_concurrent_max(&func)
    }

    fn extract_concurrent_max(func: &Bound<'_, PyAny>) -> PyResult<Option<usize>> {
        if let Ok(value) = func.getattr("__cog_concurrent_max__") {
            return value.extract::<usize>().map(Some);
        }
        if let Ok(raw_func) = func.getattr("__func__")
            && let Ok(value) = raw_func.getattr("__cog_concurrent_max__")
        {
            return value.extract::<usize>().map(Some);
        }
        Ok(None)
    }

    pub fn concurrent_max(&self) -> Option<usize> {
        self.concurrent_max
    }

    pub fn concurrent_max_from_ref(py: Python<'_>, predictor_ref: &str) -> PyResult<Option<usize>> {
        let (module_path, explicit_name) = match predictor_ref.split_once(':') {
            Some((module_path, "")) => (module_path, None),
            Some((module_path, name)) => (module_path, Some(name)),
            None => (predictor_ref, None),
        };
        let source = std::fs::read_to_string(module_path).map_err(|err| {
            PyIOError::new_err(format!(
                "failed to read predictor source {module_path}: {err}"
            ))
        })?;

        let globals = PyDict::new(py);
        py.run(
            c"\
import ast

def _cog_concurrent_max_from_source(source, explicit_name):
    tree = ast.parse(source)
    definitions = {
        node.name: node
        for node in tree.body
        if isinstance(node, (ast.ClassDef, ast.FunctionDef, ast.AsyncFunctionDef))
    }
    def bound_names(node):
        names = set()
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
            names.add(node.name)
        elif isinstance(node, ast.Import):
            for alias in node.names:
                names.add(alias.asname or alias.name.split('.')[0])
        elif isinstance(node, ast.ImportFrom):
            for alias in node.names:
                names.add(alias.asname or alias.name)
        elif isinstance(node, ast.Assign):
            for target in node.targets:
                if isinstance(target, ast.Name):
                    names.add(target.id)
        elif isinstance(node, ast.AnnAssign) and isinstance(node.target, ast.Name):
            names.add(node.target.id)
        elif isinstance(node, ast.AugAssign) and isinstance(node.target, ast.Name):
            names.add(node.target.id)
        return names

    def bindings_before(lineno, class_body=None):
        cog_aliases = set()
        concurrent_aliases = set()
        for node in tree.body:
            if getattr(node, 'lineno', 0) >= lineno:
                break
            if isinstance(node, ast.Import):
                for alias in node.names:
                    local_name = alias.asname or alias.name.split('.')[0]
                    cog_aliases.discard(local_name)
                    concurrent_aliases.discard(local_name)
                    if alias.name == 'cog':
                        cog_aliases.add(local_name)
            elif isinstance(node, ast.ImportFrom):
                for alias in node.names:
                    local_name = alias.asname or alias.name
                    cog_aliases.discard(local_name)
                    concurrent_aliases.discard(local_name)
                    if node.module == 'cog' and alias.name == 'concurrent':
                        concurrent_aliases.add(local_name)

            for name in bound_names(node):
                if isinstance(node, (ast.Import, ast.ImportFrom)):
                    continue
                cog_aliases.discard(name)
                concurrent_aliases.discard(name)
        if class_body is not None:
            for node in class_body:
                if getattr(node, 'lineno', 0) >= lineno:
                    break
                for name in bound_names(node):
                    cog_aliases.discard(name)
                    concurrent_aliases.discard(name)
        return cog_aliases, concurrent_aliases

    def base_name(base):
        if isinstance(base, ast.Name):
            return base.id
        if isinstance(base, ast.Attribute):
            return base.attr
        return None

    def constants_before(lineno):
        constants = {}
        for node in tree.body:
            if getattr(node, 'lineno', 0) >= lineno:
                break
            names = bound_names(node)
            if not names:
                continue

            if isinstance(node, ast.Assign) and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name):
                name = node.targets[0].id
                if isinstance(node.value, ast.Constant) and type(node.value.value) is int:
                    constants[name] = node.value.value
                else:
                    constants.pop(name, None)
                continue

            if isinstance(node, ast.AnnAssign) and isinstance(node.target, ast.Name):
                if node.value is None:
                    continue
                name = node.target.id
                if isinstance(node.value, ast.Constant) and type(node.value.value) is int:
                    constants[name] = node.value.value
                else:
                    constants.pop(name, None)
                continue

            for name in names:
                constants.pop(name, None)
        return constants

    def max_value(node, lineno):
        if isinstance(node, ast.Constant) and type(node.value) is int:
            value = node.value
        elif isinstance(node, ast.UnaryOp) and isinstance(node.op, ast.USub) and isinstance(node.operand, ast.Constant) and type(node.operand.value) is int:
            value = -node.operand.value
        elif isinstance(node, ast.Name):
            constants = constants_before(lineno)
            if node.id not in constants:
                raise ValueError('max must be an integer literal or module-level integer constant')
            value = constants[node.id]
        else:
            raise ValueError('max must be an integer literal or module-level integer constant')
        if value < 1:
            raise ValueError('max must be at least 1')
        return value

    def decorator_max(decorators, class_body=None):
        for decorator in decorators:
            call = decorator if isinstance(decorator, ast.Call) else None
            target = call.func if call is not None else decorator
            cog_aliases, concurrent_aliases = bindings_before(decorator.lineno, class_body)
            if isinstance(target, ast.Name):
                if target.id in concurrent_aliases:
                    is_concurrent = True
                elif target.id == 'concurrent':
                    raise ValueError('concurrent decorator is not imported from cog')
                else:
                    is_concurrent = False
            elif isinstance(target, ast.Attribute) and isinstance(target.value, ast.Name):
                if target.attr == 'concurrent' and target.value.id in cog_aliases:
                    is_concurrent = True
                elif target.attr == 'concurrent' and target.value.id == 'cog':
                    raise ValueError('concurrent decorator is not imported from cog')
                else:
                    is_concurrent = False
            else:
                is_concurrent = False
            if not is_concurrent:
                continue
            if call is None:
                return 1
            if call.args:
                raise ValueError('concurrent decorator arguments must be literal')
            for keyword in call.keywords:
                if keyword.arg is None:
                    raise ValueError('concurrent decorator arguments must be literal')
                if keyword.arg == 'max':
                    return max_value(keyword.value, decorator.lineno)
            return 1
        return None

    def direct_methods(class_node):
        return [
            node
            for node in class_node.body
            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)) and node.name in {'run', 'predict'}
        ]

    def selected_method(class_node, seen=None):
        methods = direct_methods(class_node)
        if len(methods) > 1:
            return None

        seen = set() if seen is None else seen
        if class_node.name in seen:
            return None
        seen.add(class_node.name)

        inherited = []
        for base in class_node.bases:
            name = base_name(base)
            if name in {'BaseRunner', 'BasePredictor', 'object'}:
                break
            base_node = definitions.get(name)
            if isinstance(base_node, ast.ClassDef):
                method = selected_method(base_node, seen)
                if method is not None:
                    inherited.append(method)
        candidates = methods + inherited
        method_names = {method.name for method in candidates}
        if len(candidates) == 1 or len(method_names) == 1:
            return methods[0] if methods else candidates[0]
        return None

    def class_max(class_node):
        method = selected_method(class_node)
        if method is None:
            return None
        class_body = class_node.body
        for owner in definitions.values():
            if isinstance(owner, ast.ClassDef) and any(item is method for item in owner.body):
                class_body = owner.body
                break
        return decorator_max(method.decorator_list, class_body)

    def object_max(name):
        node = definitions.get(name)
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            return decorator_max(node.decorator_list)
        if isinstance(node, ast.ClassDef):
            return class_max(node)
        return None

    if explicit_name:
        return object_max(explicit_name)

    runner = definitions.get('Runner')
    if isinstance(runner, (ast.FunctionDef, ast.AsyncFunctionDef)):
        return decorator_max(runner.decorator_list)
    if isinstance(runner, ast.ClassDef) and selected_method(runner) is not None:
        return class_max(runner)

    predictor = definitions.get('Predictor')
    if isinstance(predictor, ast.ClassDef):
        return class_max(predictor)
    return None
",
            Some(&globals),
            None,
        )?;
        let helper = globals
            .get_item("_cog_concurrent_max_from_source")?
            .expect("helper should be defined");
        helper.call1((source, explicit_name))?.extract()
    }

    fn selected_predict_method_name(
        py: Python<'_>,
        instance: &PyObject,
    ) -> PyResult<PredictMethodName> {
        let class = instance.bind(py).getattr("__class__")?;
        Self::selected_predict_method_name_for_class(py, &class)
    }

    fn selected_predict_method_name_for_class(
        py: Python<'_>,
        class: &Bound<'_, PyAny>,
    ) -> PyResult<PredictMethodName> {
        let mro = class.getattr("__mro__")?.cast_into::<PyTuple>()?;
        let cog_predictor = py.import("cog.predictor")?;
        let base_runner = cog_predictor.getattr("BaseRunner")?;
        let base_predictor = cog_predictor.getattr("BasePredictor")?;
        let builtins = py.import("builtins")?;
        let object = builtins.getattr("object")?;
        let callable = builtins.getattr("callable")?;

        let mut has_run = false;
        let mut has_predict = false;
        for owner in mro.iter() {
            if owner.is(&base_runner) || owner.is(&base_predictor) || owner.is(&object) {
                // Stop at framework classes so mixins listed after BaseRunner in a
                // subclass are not treated as selected user overrides.
                break;
            }
            let dict = owner.getattr("__dict__")?;
            let run_value = dict.call_method1("get", ("run",))?;
            if !run_value.is_none() && callable.call1((&run_value,))?.extract()? {
                has_run = true;
            }
            let predict_value = dict.call_method1("get", ("predict",))?;
            if !predict_value.is_none() && callable.call1((&predict_value,))?.extract()? {
                has_predict = true;
            }
        }

        match (has_run, has_predict) {
            (true, true) => Err(PyValueError::new_err(
                "predictor must define either run() or predict(), not both",
            )),
            (true, false) => Ok(PredictMethodName::Run),
            (false, true) => {
                tracing::warn!("predict() is deprecated; use run() instead");
                Ok(PredictMethodName::Predict)
            }
            (false, false) => Err(PyValueError::new_err(
                "run() or predict() method not found on predictor",
            )),
        }
    }

    /// Replace FieldInfo defaults with their `.default` values on a method's signature.
    ///
    /// When users write `def run/predict(self, seed: int = Input(default=42, description="..."))`,
    /// the Python default for `seed` is a `FieldInfo(default=42, ...)` object. If `seed` is
    /// missing from the input dict, Python would use this FieldInfo as the value — not `42`.
    ///
    /// This patches `__defaults__` on the underlying function so Python natively resolves
    /// to the actual default values.
    fn unwrap_field_info_defaults(
        py: Python<'_>,
        instance: &PyObject,
        method_name: &str,
    ) -> PyResult<()> {
        let field_info_class = py.import("cog.input")?.getattr("FieldInfo")?;

        // Get the underlying function object
        let func = if method_name.is_empty() {
            // Standalone function
            instance.bind(py).clone()
        } else {
            // Bound method — get __func__ for the raw function
            instance
                .bind(py)
                .getattr(method_name)?
                .getattr("__func__")?
        };

        // Patch __defaults__ (positional parameter defaults)
        if let Ok(defaults) = func.getattr("__defaults__")
            && !defaults.is_none()
        {
            let defaults_tuple = defaults.cast::<pyo3::types::PyTuple>()?;
            let mut new_defaults: Vec<Bound<'_, PyAny>> = Vec::new();
            let mut changed = false;

            for item in defaults_tuple.iter() {
                if item.is_instance(&field_info_class)? {
                    new_defaults.push(item.getattr("default")?);
                    changed = true;
                } else {
                    new_defaults.push(item);
                }
            }

            if changed {
                let new_tuple = pyo3::types::PyTuple::new(py, &new_defaults)?;
                func.setattr("__defaults__", new_tuple)?;
                tracing::debug!("Patched FieldInfo defaults on {}", method_name);
            }
        }

        // Patch __kwdefaults__ (keyword-only parameter defaults)
        if let Ok(kwdefaults) = func.getattr("__kwdefaults__")
            && !kwdefaults.is_none()
        {
            let kwdefaults_dict = kwdefaults.cast::<pyo3::types::PyDict>()?;
            let mut changed = false;

            for (key, value) in kwdefaults_dict.iter() {
                if value.is_instance(&field_info_class)? {
                    kwdefaults_dict.set_item(&key, value.getattr("default")?)?;
                    changed = true;
                }
            }

            if changed {
                tracing::debug!("Patched FieldInfo kwdefaults on {}", method_name);
            }
        }

        Ok(())
    }

    /// Detect if a method is an async function.
    /// Returns (is_async, is_async_gen) tuple.
    ///
    /// If method_name is empty, checks the instance itself (for standalone functions).
    fn detect_async(
        py: Python<'_>,
        instance: &PyObject,
        method_name: &str,
    ) -> PyResult<(bool, bool)> {
        let inspect = py.import("inspect")?;

        // If method_name is empty, check the instance itself (standalone function)
        let target = if method_name.is_empty() {
            instance.bind(py).clone()
        } else {
            instance.bind(py).getattr(method_name)?
        };

        // Check isasyncgenfunction first (it's more specific)
        let is_async_gen: bool = inspect
            .call_method1("isasyncgenfunction", (&target,))?
            .extract()?;
        if is_async_gen {
            return Ok((true, true));
        }

        // Check iscoroutinefunction
        let is_coro: bool = inspect
            .call_method1("iscoroutinefunction", (&target,))?
            .extract()?;
        Ok((is_coro, false))
    }

    /// Returns true if this predictor has an async run()/predict() method.
    pub fn is_async(&self) -> bool {
        match &self.kind {
            PredictorKind::Class { predict, .. } => {
                matches!(predict, PredictKind::Async | PredictKind::AsyncGen)
            }
            PredictorKind::StandaloneFunction(predict_kind) => {
                matches!(predict_kind, PredictKind::Async | PredictKind::AsyncGen)
            }
        }
    }

    /// Returns true if this predictor has a train() method.
    pub fn has_train(&self) -> bool {
        match &self.kind {
            PredictorKind::Class { train, .. } => !matches!(train, TrainKind::None),
            PredictorKind::StandaloneFunction(_) => true,
        }
    }

    /// Returns true if the train() method is async.
    pub fn is_train_async(&self) -> bool {
        match &self.kind {
            PredictorKind::Class { train, .. } => matches!(train, TrainKind::Async),
            PredictorKind::StandaloneFunction(predict_kind) => {
                matches!(predict_kind, PredictKind::Async | PredictKind::AsyncGen)
            }
        }
    }

    /// Call setup() on the predictor, handling weights parameter if present.
    ///
    /// Uses cog.predictor helpers to detect and extract weights:
    /// - `has_setup_weights()` checks if setup() has a weights parameter
    /// - `extract_setup_weights()` reads from COG_WEIGHTS env or ./weights path
    ///
    /// If setup() is an async def and an event loop is provided, the coroutine
    /// is submitted to that loop via `run_coroutine_threadsafe` so that
    /// event-loop-bound resources created during setup (httpx.AsyncClient, etc.)
    /// remain usable in predict(). If no loop is provided, falls back to
    /// `asyncio.run()` (used by the non-worker code path).
    pub fn setup(&self, py: Python<'_>, event_loop: Option<&Py<PyAny>>) -> PyResult<()> {
        let instance = self.instance.bind(py);

        // Check if setup method exists
        if !instance.hasattr("setup")? {
            return Ok(());
        }

        // Import cog.predictor helpers
        let cog_predictor = py.import("cog.predictor")?;
        let has_setup_weights = cog_predictor.getattr("has_setup_weights")?;
        let extract_setup_weights = cog_predictor.getattr("extract_setup_weights")?;

        // Check if setup() has a weights parameter
        let needs_weights: bool = has_setup_weights.call1((&instance,))?.extract()?;

        let result = if needs_weights {
            // Extract weights from COG_WEIGHTS env or ./weights path
            let weights = extract_setup_weights.call1((&instance,))?;
            instance.call_method1("setup", (weights,))?
        } else {
            instance.call_method0("setup")?
        };

        // If setup() is async, the call above returns a coroutine — run it.
        if self.setup_is_async {
            let asyncio = py.import("asyncio")?;
            match event_loop {
                Some(loop_obj) => {
                    // Submit to the shared event loop so setup and predict share
                    // the same loop. This keeps event-loop-bound resources alive.
                    let future = asyncio
                        .call_method1("run_coroutine_threadsafe", (&result, loop_obj.bind(py)))?;
                    // Block until setup completes (preserves existing semantics).
                    future.call_method0("result")?;
                }
                None => {
                    // No shared loop (non-worker path) — use ephemeral loop.
                    asyncio.call_method1("run", (&result,))?;
                }
            }
        }

        Ok(())
    }

    /// Get the predict function object for type annotation introspection.
    pub fn predict_func<'py>(&self, py: Python<'py>) -> PyResult<Bound<'py, PyAny>> {
        let instance = self.instance.bind(py);
        match &self.kind {
            PredictorKind::Class { method_name, .. } => instance.getattr(method_name.as_str()),
            PredictorKind::StandaloneFunction(_) => Ok(instance.clone()),
        }
    }

    /// Get the train function object for type annotation introspection.
    pub fn train_func<'py>(&self, py: Python<'py>) -> PyResult<Bound<'py, PyAny>> {
        let instance = self.instance.bind(py);
        match &self.kind {
            PredictorKind::Class { .. } => instance.getattr("train"),
            PredictorKind::StandaloneFunction(_) => Ok(instance.clone()),
        }
    }

    /// Call run()/predict() with the given input dict, returning raw Python output.
    ///
    /// For standalone functions, calls the function directly.
    pub fn predict_raw(&self, py: Python<'_>, input: &Bound<'_, PyDict>) -> PyResult<PyObject> {
        let (method_name, is_async) = match &self.kind {
            PredictorKind::Class {
                method_name,
                predict,
                ..
            } => (
                method_name.as_str(),
                matches!(predict, PredictKind::Async | PredictKind::AsyncGen),
            ),
            PredictorKind::StandaloneFunction(predict_kind) => (
                "",
                matches!(predict_kind, PredictKind::Async | PredictKind::AsyncGen),
            ),
        };
        self.call_method_raw(py, method_name, is_async, input)
    }

    /// Call train() with the given input dict, returning raw Python output.
    ///
    /// For standalone train functions, calls the function directly.
    /// For Predictor classes with a train() method, calls instance.train().
    pub fn train_raw(&self, py: Python<'_>, input: &Bound<'_, PyDict>) -> PyResult<PyObject> {
        let (method_name, is_async) = match &self.kind {
            PredictorKind::Class { train, .. } => ("train", matches!(train, TrainKind::Async)),
            PredictorKind::StandaloneFunction(predict_kind) => (
                "",
                matches!(predict_kind, PredictKind::Async | PredictKind::AsyncGen),
            ),
        };
        self.call_method_raw(py, method_name, is_async, input)
    }

    /// Internal helper to call a method (predict or train) on the predictor.
    fn call_method_raw(
        &self,
        py: Python<'_>,
        method_name: &str,
        is_async: bool,
        input: &Bound<'_, PyDict>,
    ) -> PyResult<PyObject> {
        let instance = self.instance.bind(py);

        // Call the method - returns coroutine if async, result if sync
        // If method_name is empty, call the instance directly (standalone function)
        let method_result = if method_name.is_empty() {
            instance.call((), Some(input))?
        } else {
            instance.call_method(method_name, (), Some(input))?
        };

        // If async, run the coroutine with asyncio.run()
        let result = if is_async {
            let asyncio = py.import("asyncio")?;
            asyncio.call_method1("run", (&method_result,))?
        } else {
            method_result
        };

        Ok(result.unbind())
    }

    /// Worker mode predict - with input processing and output serialization.
    pub fn predict_worker(
        &self,
        input: serde_json::Value,
        slot_sender: Arc<SlotSender>,
    ) -> Result<PredictionResult, PredictionError> {
        Python::attach(|py| {
            let json_module = py.import("json").map_err(|e| {
                PredictionError::Failed(format!("Failed to import json module: {}", e))
            })?;
            let types_module = py.import("types").map_err(|e| {
                PredictionError::Failed(format!("Failed to import types module: {}", e))
            })?;
            let generator_type = types_module.getattr("GeneratorType").map_err(|e| {
                PredictionError::Failed(format!("Failed to get GeneratorType: {}", e))
            })?;

            let input_str = serde_json::to_string(&input)
                .map_err(|e| PredictionError::InvalidInput(e.to_string()))?;

            let py_input = json_module
                .call_method1("loads", (input_str,))
                .map_err(|e| PredictionError::InvalidInput(format!("Invalid JSON input: {}", e)))?;

            #[allow(deprecated)]
            let raw_input_dict = py_input.downcast::<PyDict>().map_err(|_| {
                PredictionError::InvalidInput("Input must be a JSON object".to_string())
            })?;

            // PreparedInput cleans up temp files on drop (RAII)
            let func = self.predict_func(py).map_err(|e| {
                PredictionError::Failed(format!("Failed to get predict function: {}", e))
            })?;
            let prepared = input::prepare_input(py, raw_input_dict, &func)
                .map_err(|e| PredictionError::InvalidInput(format_validation_error(py, &e)))?;
            let input_dict = prepared.dict(py);

            // Call predict
            let result = self.predict_raw(py, &input_dict);

            // Handle errors (prepared drops here, cleaning up temp files)
            let result = match result {
                Ok(r) => r,
                Err(e) => {
                    drop(prepared); // Explicit cleanup on error path
                    if is_cancelation_exception(py, &e) {
                        return Err(PredictionError::Cancelled);
                    }
                    return Err(PredictionError::Failed(format!("Prediction failed: {}", e)));
                }
            };

            let result_bound = result.bind(py);
            let is_generator: bool = result_bound.is_instance(&generator_type).unwrap_or(false);

            let output = if is_generator {
                self.process_generator_output(py, result_bound, &json_module, &slot_sender)?
            } else {
                self.process_single_output(py, result_bound, &json_module, &slot_sender)?
            };

            // prepared drops here, cleaning up temp files via RAII
            drop(prepared);

            Ok(PredictionResult {
                output,
                predict_time: None,
                logs: String::new(),
                metrics: Default::default(),
            })
        })
    }

    /// Worker mode train - with input processing and output serialization.
    pub fn train_worker(
        &self,
        input: serde_json::Value,
        slot_sender: Arc<SlotSender>,
    ) -> Result<PredictionResult, PredictionError> {
        Python::attach(|py| {
            let json_module = py.import("json").map_err(|e| {
                PredictionError::Failed(format!("Failed to import json module: {}", e))
            })?;
            let types_module = py.import("types").map_err(|e| {
                PredictionError::Failed(format!("Failed to import types module: {}", e))
            })?;
            let generator_type = types_module.getattr("GeneratorType").map_err(|e| {
                PredictionError::Failed(format!("Failed to get GeneratorType: {}", e))
            })?;

            let input_str = serde_json::to_string(&input)
                .map_err(|e| PredictionError::InvalidInput(e.to_string()))?;

            let py_input = json_module
                .call_method1("loads", (input_str,))
                .map_err(|e| PredictionError::InvalidInput(format!("Invalid JSON input: {}", e)))?;

            #[allow(deprecated)]
            let raw_input_dict = py_input.downcast::<PyDict>().map_err(|_| {
                PredictionError::InvalidInput("Input must be a JSON object".to_string())
            })?;

            // PreparedInput cleans up temp files on drop (RAII)
            let func = self.train_func(py).map_err(|e| {
                PredictionError::Failed(format!("Failed to get train function: {}", e))
            })?;
            let prepared = input::prepare_input(py, raw_input_dict, &func)
                .map_err(|e| PredictionError::InvalidInput(format_validation_error(py, &e)))?;
            let input_dict = prepared.dict(py);

            // Call train
            let result = self.train_raw(py, &input_dict);

            // Handle errors
            let result = match result {
                Ok(r) => r,
                Err(e) => {
                    drop(prepared);
                    if is_cancelation_exception(py, &e) {
                        return Err(PredictionError::Cancelled);
                    }
                    return Err(PredictionError::Failed(format!("Training failed: {}", e)));
                }
            };

            let result_bound = result.bind(py);
            let is_generator: bool = result_bound.is_instance(&generator_type).unwrap_or(false);

            let output = if is_generator {
                self.process_generator_output(py, result_bound, &json_module, &slot_sender)?
            } else {
                self.process_single_output(py, result_bound, &json_module, &slot_sender)?
            };

            drop(prepared);

            Ok(PredictionResult {
                output,
                predict_time: None,
                logs: String::new(),
                metrics: Default::default(),
            })
        })
    }

    /// Process generator output by streaming each yield over IPC.
    fn process_generator_output(
        &self,
        py: Python<'_>,
        result: &Bound<'_, PyAny>,
        json_module: &Bound<'_, PyAny>,
        slot_sender: &SlotSender,
    ) -> Result<PredictionOutput, PredictionError> {
        let iter = result
            .try_iter()
            .map_err(|e| PredictionError::Failed(format!("Failed to iterate generator: {}", e)))?;

        for item in iter {
            let item = item.map_err(|e| {
                if is_cancelation_exception(py, &e) {
                    return PredictionError::Cancelled;
                }
                PredictionError::Failed(format!("Generator iteration error: {}", e))
            })?;

            send_output_item(py, &item, json_module, slot_sender)?;
        }

        // Outputs already streamed over IPC — return empty stream
        Ok(PredictionOutput::Stream(vec![]))
    }

    /// Process single output into PredictionOutput::Single.
    ///
    /// For file outputs (Path/IOBase), the file is sent via slot_sender and
    /// an empty Single(Null) is returned since the output was already streamed.
    fn process_single_output(
        &self,
        py: Python<'_>,
        result: &Bound<'_, PyAny>,
        json_module: &Bound<'_, PyAny>,
        slot_sender: &SlotSender,
    ) -> Result<PredictionOutput, PredictionError> {
        // Check for file-type outputs first
        let os = py
            .import("os")
            .map_err(|e| PredictionError::Failed(format!("Failed to import os: {}", e)))?;
        let io_mod = py
            .import("io")
            .map_err(|e| PredictionError::Failed(format!("Failed to import io: {}", e)))?;
        let pathlike = os
            .getattr("PathLike")
            .map_err(|e| PredictionError::Failed(format!("Failed to get os.PathLike: {}", e)))?;
        let iobase = io_mod
            .getattr("IOBase")
            .map_err(|e| PredictionError::Failed(format!("Failed to get io.IOBase: {}", e)))?;

        if result.is_instance(&pathlike).unwrap_or(false) {
            let path_str: String = result
                .call_method0("__fspath__")
                .and_then(|p| p.extract())
                .map_err(|e| PredictionError::Failed(format!("Failed to get fspath: {}", e)))?;
            slot_sender
                .send_file_output(std::path::PathBuf::from(path_str), None)
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to send file output: {}", e))
                })?;
            return Ok(PredictionOutput::Single(serde_json::Value::Null));
        }

        if result.is_instance(&iobase).unwrap_or(false) {
            if result
                .call_method0("seekable")
                .and_then(|r| r.extract::<bool>())
                .unwrap_or(false)
            {
                let _ = result.call_method1("seek", (0,));
            }
            let data: Vec<u8> = result
                .call_method0("read")
                .and_then(|d| d.extract())
                .map_err(|e| PredictionError::Failed(format!("Failed to read IOBase: {}", e)))?;
            let ext = result
                .getattr("name")
                .and_then(|n| n.extract::<String>())
                .ok()
                .and_then(|name| {
                    std::path::Path::new(&name)
                        .extension()
                        .and_then(|e| e.to_str())
                        .map(|s| s.to_string())
                })
                .unwrap_or_else(|| "bin".to_string());
            slot_sender
                .write_file_output(&data, &ext, None)
                .map_err(|e| {
                    PredictionError::Failed(format!("Failed to write file output: {}", e))
                })?;
            return Ok(PredictionOutput::Single(serde_json::Value::Null));
        }

        // List/tuple output — iterate items so file outputs (Path, IOBase)
        // go through the FileOutput IPC path for upload instead of being
        // base64-encoded inline by process_output.
        if let Ok(list) = result.cast::<pyo3::types::PyList>() {
            for item in list.iter() {
                send_output_item(py, &item, json_module, slot_sender)?;
            }
            return Ok(PredictionOutput::Stream(vec![]));
        }
        if let Ok(tuple) = result.cast::<pyo3::types::PyTuple>() {
            for item in tuple.iter() {
                send_output_item(py, &item, json_module, slot_sender)?;
            }
            return Ok(PredictionOutput::Stream(vec![]));
        }

        // Non-file output — process normally
        let processed = output::process_output(py, result)
            .map_err(|e| PredictionError::Failed(format!("Failed to process output: {}", e)))?;

        let result_str: String = json_module
            .call_method1("dumps", (&processed,))
            .map_err(|e| PredictionError::Failed(format!("Failed to serialize output: {}", e)))?
            .extract()
            .map_err(|e| {
                PredictionError::Failed(format!("Failed to extract output string: {}", e))
            })?;

        let output_json: serde_json::Value = serde_json::from_str(&result_str)
            .map_err(|e| PredictionError::Failed(format!("Failed to parse output JSON: {}", e)))?;

        Ok(PredictionOutput::Single(output_json))
    }

    /// Worker mode async predict - submits to shared event loop.
    ///
    /// Uses run_coroutine_threadsafe to submit the coroutine to the provided event loop.
    /// Returns the concurrent.futures.Future, is_async_gen flag, and PreparedInput for cleanup.
    /// Caller should block on future.result() to get the result, then drop PreparedInput.
    ///
    /// The prediction_id is used to set up log routing in the event loop thread.
    /// The scope is used to propagate metric recording to the event loop thread.
    pub fn predict_async_worker(
        &self,
        input: serde_json::Value,
        event_loop: &Py<PyAny>,
        prediction_id: &str,
        scope: Option<&Py<crate::metric_scope::Scope>>,
    ) -> Result<(Py<PyAny>, bool, PreparedInput), PredictionError> {
        Python::attach(|py| {
            let json_module = py.import("json").map_err(|e| {
                PredictionError::Failed(format!("Failed to import json module: {}", e))
            })?;

            let input_str = serde_json::to_string(&input)
                .map_err(|e| PredictionError::InvalidInput(e.to_string()))?;
            let py_input = json_module
                .call_method1("loads", (input_str,))
                .map_err(|e| PredictionError::InvalidInput(format!("Invalid JSON input: {}", e)))?;

            #[allow(deprecated)]
            let raw_input_dict = py_input.downcast::<PyDict>().map_err(|_| {
                PredictionError::InvalidInput("Input must be a JSON object".to_string())
            })?;

            let func = self.predict_func(py).map_err(|e| {
                PredictionError::Failed(format!("Failed to get predict function: {}", e))
            })?;
            let prepared = input::prepare_input(py, raw_input_dict, &func)
                .map_err(|e| PredictionError::InvalidInput(format_validation_error(py, &e)))?;
            let input_dict = prepared.dict(py);

            // Call run()/predict() - returns coroutine
            let instance = self.instance.bind(py);
            let method_name = match &self.kind {
                PredictorKind::Class { method_name, .. } => method_name.as_str(),
                PredictorKind::StandaloneFunction(_) => "standalone function",
            };
            let coro = match &self.kind {
                PredictorKind::StandaloneFunction(_) => instance.call((), Some(&input_dict)),
                PredictorKind::Class { .. } => {
                    instance.call_method(method_name, (), Some(&input_dict))
                }
            }
            .map_err(|e| PredictionError::Failed(format!("Failed to call {method_name}: {e}")))?;

            // For async generators, wrap to collect all values
            let is_async_gen = matches!(
                &self.kind,
                PredictorKind::Class {
                    predict: PredictKind::AsyncGen,
                    ..
                } | PredictorKind::StandaloneFunction(PredictKind::AsyncGen)
            );
            let coro = if is_async_gen {
                let collect_fn = get_collect_async_gen(py)?;
                collect_fn
                    .call1(py, (&coro,))
                    .map_err(|e| {
                        PredictionError::Failed(format!("Failed to wrap async generator: {}", e))
                    })?
                    .into_bound(py)
            } else {
                coro
            };

            // Wrap coroutine with log + metric context and submit to event loop
            let future = submit_async_coroutine(py, &coro, event_loop, prediction_id, scope)?;

            Ok((future, is_async_gen, prepared))
        })
    }

    /// Process the result from an async prediction future.
    ///
    /// Call this after future.result() returns to convert the Python result
    /// to a PredictionResult.
    pub fn process_async_result(
        &self,
        py: Python<'_>,
        result: &Bound<'_, PyAny>,
        is_async_gen: bool,
        slot_sender: &SlotSender,
    ) -> Result<PredictionResult, PredictionError> {
        let json_module = py
            .import("json")
            .map_err(|e| PredictionError::Failed(format!("Failed to import json module: {}", e)))?;
        let types_module = py.import("types").map_err(|e| {
            PredictionError::Failed(format!("Failed to import types module: {}", e))
        })?;

        // Process output
        let output = if is_async_gen {
            // Result is a pre-collected list — stream each item over IPC
            if let Ok(list) = result.extract::<Vec<Bound<'_, PyAny>>>() {
                for item in list {
                    send_output_item(py, &item, &json_module, slot_sender)?;
                }
            }
            PredictionOutput::Stream(vec![])
        } else {
            // Check if result is a generator (sync generator from async predict)
            let generator_type = types_module.getattr("GeneratorType").map_err(|e| {
                PredictionError::Failed(format!("Failed to get GeneratorType: {}", e))
            })?;
            let is_generator: bool = result.is_instance(&generator_type).unwrap_or(false);

            if is_generator {
                self.process_generator_output(py, result, &json_module, slot_sender)?
            } else {
                self.process_single_output(py, result, &json_module, slot_sender)?
            }
        };

        Ok(PredictionResult {
            output,
            predict_time: None,
            logs: String::new(),
            metrics: Default::default(),
        })
    }

    /// Worker mode async train - submits to shared event loop.
    pub fn train_async_worker(
        &self,
        input: serde_json::Value,
        event_loop: &Py<PyAny>,
        prediction_id: &str,
        scope: Option<&Py<crate::metric_scope::Scope>>,
    ) -> Result<(Py<PyAny>, bool, PreparedInput), PredictionError> {
        Python::attach(|py| {
            let json_module = py.import("json").map_err(|e| {
                PredictionError::Failed(format!("Failed to import json module: {}", e))
            })?;

            let input_str = serde_json::to_string(&input)
                .map_err(|e| PredictionError::InvalidInput(e.to_string()))?;
            let py_input = json_module
                .call_method1("loads", (input_str,))
                .map_err(|e| PredictionError::InvalidInput(format!("Invalid JSON input: {}", e)))?;

            #[allow(deprecated)]
            let raw_input_dict = py_input.downcast::<PyDict>().map_err(|_| {
                PredictionError::InvalidInput("Input must be a JSON object".to_string())
            })?;

            let func = self.train_func(py).map_err(|e| {
                PredictionError::Failed(format!("Failed to get train function: {}", e))
            })?;
            let prepared = input::prepare_input(py, raw_input_dict, &func)
                .map_err(|e| PredictionError::InvalidInput(format_validation_error(py, &e)))?;
            let input_dict = prepared.dict(py);

            // Call train - returns coroutine
            let instance = self.instance.bind(py);
            let coro = match &self.kind {
                PredictorKind::StandaloneFunction(_) => instance.call((), Some(&input_dict)),
                PredictorKind::Class { .. } => instance.call_method("train", (), Some(&input_dict)),
            }
            .map_err(|e| PredictionError::Failed(format!("Failed to call train: {}", e)))?;

            // Wrap coroutine with log + metric context and submit to event loop
            let future = submit_async_coroutine(py, &coro, event_loop, prediction_id, scope)?;

            // Train doesn't typically use async generators, but we return false for consistency
            Ok((future, false, prepared))
        })
    }

    // =========================================================================
    // Healthcheck methods
    // =========================================================================

    /// Healthcheck timeout in seconds.
    const HEALTHCHECK_TIMEOUT: f64 = 5.0;

    /// Check if the predictor has a healthcheck() method.
    pub fn has_healthcheck(&self, py: Python<'_>) -> bool {
        match &self.kind {
            PredictorKind::Class { .. } => {
                let instance = self.instance.bind(py);
                instance.hasattr("healthcheck").unwrap_or(false)
            }
            PredictorKind::StandaloneFunction(_) => false,
        }
    }

    /// Check if the healthcheck() method is async.
    pub fn is_healthcheck_async(&self, py: Python<'_>) -> bool {
        match &self.kind {
            PredictorKind::Class { .. } => {
                let instance = self.instance.bind(py);
                if let Ok(healthcheck) = instance.getattr("healthcheck") {
                    let inspect = py.import("inspect").ok();
                    if let Some(inspect) = inspect {
                        inspect
                            .call_method1("iscoroutinefunction", (&healthcheck,))
                            .ok()
                            .and_then(|r| r.extract::<bool>().ok())
                            .unwrap_or(false)
                    } else {
                        false
                    }
                } else {
                    false
                }
            }
            PredictorKind::StandaloneFunction(_) => false,
        }
    }

    /// Run a synchronous healthcheck with timeout.
    ///
    /// Runs the healthcheck in a thread pool executor with a 5 second timeout.
    pub fn healthcheck_sync(&self, py: Python<'_>) -> coglet_core::orchestrator::HealthcheckResult {
        use coglet_core::orchestrator::HealthcheckResult;

        let instance = self.instance.bind(py);

        // Run healthcheck in executor with timeout, mirroring Python impl
        let result: PyResult<bool> = (|| {
            let concurrent_futures = py.import("concurrent.futures")?;
            let thread_pool = concurrent_futures.getattr("ThreadPoolExecutor")?;

            // Create a small executor just for this healthcheck
            let executor = thread_pool.call1((1,))?;

            // Get the healthcheck method
            let healthcheck_fn = instance.getattr("healthcheck")?;

            // Submit to executor
            let future = executor.call_method1("submit", (healthcheck_fn,))?;

            // Wait with timeout
            let result = future.call_method1("result", (Self::HEALTHCHECK_TIMEOUT,));

            // Shutdown executor
            let _ = executor.call_method1("shutdown", (false,));

            match result {
                Ok(r) => Ok(r.extract::<bool>().unwrap_or(true)),
                Err(e) => {
                    let err_str = e.to_string();
                    if err_str.contains("TimeoutError") {
                        Err(pyo3::exceptions::PyTimeoutError::new_err(
                            "Healthcheck timed out",
                        ))
                    } else {
                        Err(e)
                    }
                }
            }
        })();

        match result {
            Ok(true) => HealthcheckResult::healthy(),
            Ok(false) => HealthcheckResult::unhealthy(
                "Healthcheck failed: user-defined healthcheck returned False",
            ),
            Err(e) => {
                let err_str = e.to_string();
                if err_str.contains("TimeoutError") {
                    HealthcheckResult::unhealthy(format!(
                        "Healthcheck failed: user-defined healthcheck timed out after {:.1} seconds",
                        Self::HEALTHCHECK_TIMEOUT
                    ))
                } else {
                    HealthcheckResult::unhealthy(format!("Healthcheck failed: {}", e))
                }
            }
        }
    }

    /// Run an async healthcheck with timeout.
    ///
    /// Runs the healthcheck in the async event loop with a 5 second timeout.
    pub fn healthcheck_async(
        &self,
        py: Python<'_>,
        event_loop: &Py<PyAny>,
    ) -> coglet_core::orchestrator::HealthcheckResult {
        use coglet_core::orchestrator::HealthcheckResult;

        let instance = self.instance.bind(py);

        let result: PyResult<bool> = (|| {
            let asyncio = py.import("asyncio")?;

            // Get the healthcheck coroutine
            let healthcheck_fn = instance.getattr("healthcheck")?;
            let coro = healthcheck_fn.call0()?;

            // Wrap with timeout
            let wait_for = asyncio.getattr("wait_for")?;
            let timeout_coro = wait_for.call1((&coro, Self::HEALTHCHECK_TIMEOUT))?;

            // Submit to event loop
            let future = asyncio.call_method1(
                "run_coroutine_threadsafe",
                (&timeout_coro, event_loop.bind(py)),
            )?;

            // Block on result with extra buffer time for event loop overhead
            let result = future.call_method1("result", (Self::HEALTHCHECK_TIMEOUT + 1.0,));

            match result {
                Ok(r) => Ok(r.extract::<bool>().unwrap_or(true)),
                Err(e) => {
                    let err_str = e.to_string();
                    if err_str.contains("TimeoutError") || err_str.contains("timed out") {
                        Err(pyo3::exceptions::PyTimeoutError::new_err(
                            "Healthcheck timed out",
                        ))
                    } else {
                        Err(e)
                    }
                }
            }
        })();

        match result {
            Ok(true) => HealthcheckResult::healthy(),
            Ok(false) => HealthcheckResult::unhealthy(
                "Healthcheck failed: user-defined healthcheck returned False",
            ),
            Err(e) => {
                let err_str = e.to_string();
                if err_str.contains("TimeoutError") {
                    HealthcheckResult::unhealthy(format!(
                        "Healthcheck failed: user-defined healthcheck timed out after {:.1} seconds",
                        Self::HEALTHCHECK_TIMEOUT
                    ))
                } else {
                    HealthcheckResult::unhealthy(format!("Healthcheck failed: {}", e))
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    use std::path::PathBuf;

    use pyo3::types::PyList;

    fn add_python_sdk_path(py: Python<'_>) {
        py.run(
            c"\
import sys, types
coglet = types.ModuleType('coglet')
coglet.CancelationException = Exception
sys.modules.setdefault('coglet', coglet)
requests = types.ModuleType('requests')
sys.modules.setdefault('requests', requests)
",
            None,
            None,
        )
        .expect("failed to install coglet test stub");

        let manifest_dir = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
        let sdk_path = manifest_dir
            .parent()
            .and_then(|p| p.parent())
            .expect("crate should live under crates/coglet-python")
            .join("python");
        let sys = py.import("sys").expect("sys should import");
        let path = sys
            .getattr("path")
            .expect("sys.path should exist")
            .cast_into::<PyList>()
            .expect("sys.path should be a list");
        path.insert(0, sdk_path.to_string_lossy().as_ref())
            .expect("failed to prepend SDK path");
    }

    fn load_predictor_source(source: &str) -> PyResult<PythonPredictor> {
        pyo3::Python::initialize();
        let dir = tempfile::tempdir().expect("failed to create temp dir");
        let path = dir.path().join("predictor.py");
        std::fs::write(&path, source).expect("failed to write test predictor");
        Python::attach(|py| {
            add_python_sdk_path(py);
            let predictor_ref = format!("{}:Predictor", path.display());
            PythonPredictor::load(py, &predictor_ref)
        })
    }

    fn load_predictor_source_with_ref(source: &str, ref_name: &str) -> PyResult<PythonPredictor> {
        pyo3::Python::initialize();
        let dir = tempfile::tempdir().expect("failed to create temp dir");
        let path = dir.path().join("predictor.py");
        std::fs::write(&path, source).expect("failed to write test predictor");
        Python::attach(|py| {
            add_python_sdk_path(py);
            let predictor_ref = format!("{}:{}", path.display(), ref_name);
            PythonPredictor::load(py, &predictor_ref)
        })
    }

    fn concurrent_max_from_source(source: &str, ref_name: &str) -> PyResult<Option<usize>> {
        pyo3::Python::initialize();
        let dir = tempfile::tempdir().expect("failed to create temp dir");
        let path = dir.path().join("predictor.py");
        std::fs::write(&path, source).expect("failed to write test predictor");
        Python::attach(|py| {
            add_python_sdk_path(py);
            let predictor_ref = if ref_name.is_empty() {
                path.display().to_string()
            } else {
                format!("{}:{}", path.display(), ref_name)
            };
            PythonPredictor::concurrent_max_from_ref(py, &predictor_ref)
        })
    }

    fn selected_predict_method_name(predictor: &PythonPredictor) -> String {
        Python::attach(|py| {
            predictor
                .predict_func(py)
                .expect("predict function should exist")
                .getattr("__name__")
                .expect("predict function should have __name__")
                .extract()
                .expect("__name__ should be a string")
        })
    }

    #[test]
    fn class_with_run_loads() {
        let predictor = load_predictor_source(
            r#"
from cog import BaseRunner

class Predictor(BaseRunner):
    def run(self) -> str:
        return "ok"
"#,
        )
        .expect("predictor with run should load");

        assert_eq!(selected_predict_method_name(&predictor), "run");
    }

    #[test]
    fn class_run_concurrent_max_loads_from_decorator() {
        let predictor = load_predictor_source(
            r#"
import cog
from cog import BaseRunner

class Predictor(BaseRunner):
    @cog.concurrent(max=3)
    async def run(self) -> str:
        return "ok"
"#,
        )
        .expect("predictor with concurrent run should load");

        assert_eq!(predictor.concurrent_max(), Some(3));
    }

    #[test]
    fn concurrent_max_from_ref_does_not_instantiate_class_predictor() {
        let source = r#"
import cog
from cog import BaseRunner

class Predictor(BaseRunner):
    def __init__(self):
        raise RuntimeError("constructor should not run")

    @cog.concurrent(max=3)
    async def run(self) -> str:
        return "ok"
"#;

        let concurrent_max = concurrent_max_from_source(source, "Predictor")
            .expect("metadata inspection should not instantiate predictor");

        assert_eq!(concurrent_max, Some(3));
        assert!(
            load_predictor_source(source).is_err(),
            "full predictor load should still instantiate and fail"
        );
    }

    #[test]
    fn concurrent_max_from_ref_does_not_execute_module_top_level_code() {
        let source = r#"
import cog

raise RuntimeError("module top-level code should not run")

class Predictor:
    @cog.concurrent(max=3)
    async def run(self) -> str:
        return "ok"
"#;

        let concurrent_max = concurrent_max_from_source(source, "Predictor")
            .expect("metadata inspection should not execute predictor module");

        assert_eq!(concurrent_max, Some(3));
    }

    #[test]
    fn concurrent_max_from_ref_falls_back_to_predictor_when_runner_invalid() {
        let source = r#"
import cog

class Runner:
    def run(self) -> str:
        return "run"

    def predict(self) -> str:
        return "predict"

class Predictor:
    @cog.concurrent(max=4)
    async def run(self) -> str:
        return "ok"
"#;

        let concurrent_max = concurrent_max_from_source(source, "")
            .expect("metadata inspection should fall back to Predictor");

        assert_eq!(concurrent_max, Some(4));
    }

    #[test]
    fn concurrent_max_from_ref_supports_runner_function() {
        let source = r#"
from cog import concurrent

@concurrent(max=5)
async def Runner() -> str:
    return "ok"
"#;

        let concurrent_max = concurrent_max_from_source(source, "")
            .expect("metadata inspection should support Runner function");

        assert_eq!(concurrent_max, Some(5));
    }

    #[test]
    fn concurrent_max_from_ref_supports_import_aliases() {
        let source = r#"
import cog as c
from cog import concurrent as cog_concurrent

class Runner:
    @c.concurrent(max=4)
    async def run(self) -> str:
        return "ok"

class Predictor:
    @cog_concurrent(max=5)
    async def run(self) -> str:
        return "ok"
"#;

        let runner_max = concurrent_max_from_source(source, "Runner")
            .expect("metadata inspection should support import cog aliases");
        let predictor_max = concurrent_max_from_source(source, "Predictor")
            .expect("metadata inspection should support concurrent import aliases");

        assert_eq!(runner_max, Some(4));
        assert_eq!(predictor_max, Some(5));
    }

    #[test]
    fn concurrent_max_from_ref_rejects_user_defined_concurrent_decorator() {
        let source = r#"
def concurrent(fn=None, *, max=1):
    return fn if fn is not None else lambda inner: inner

class Predictor:
    @concurrent(max=3)
    async def run(self) -> str:
        return "ok"
"#;

        let err = concurrent_max_from_source(source, "Predictor")
            .expect_err("ambiguous concurrent decorator should be rejected");

        assert!(
            err.to_string()
                .contains("concurrent decorator is not imported from cog"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn concurrent_max_from_ref_rejects_shadowed_cog_import() {
        let source = r#"
import cog

cog = object()

class Predictor:
    @cog.concurrent(max=3)
    async def run(self) -> str:
        return "ok"
"#;

        let err = concurrent_max_from_source(source, "Predictor")
            .expect_err("shadowed cog import should be rejected");

        assert!(
            err.to_string()
                .contains("concurrent decorator is not imported from cog"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn concurrent_max_from_ref_rejects_class_body_shadowed_cog_import() {
        let source = r#"
import cog
from cog import BaseRunner

def fake_concurrent(*, max):
    return lambda fn: fn

class Predictor(BaseRunner):
    cog = type("X", (), {"concurrent": fake_concurrent})

    @cog.concurrent(max=4)
    async def run(self) -> str:
        return "ok"
"#;

        let err = concurrent_max_from_source(source, "Predictor")
            .expect_err("class-body shadowed cog import should be rejected");

        assert!(
            err.to_string()
                .contains("concurrent decorator is not imported from cog"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn concurrent_max_from_ref_rejects_rebound_concurrent_import() {
        let source = r#"
from cog import concurrent
from other import concurrent

class Predictor:
    @concurrent(max=3)
    async def run(self) -> str:
        return "ok"
"#;

        let err = concurrent_max_from_source(source, "Predictor")
            .expect_err("rebound concurrent import should be rejected");

        assert!(
            err.to_string()
                .contains("concurrent decorator is not imported from cog"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn concurrent_max_from_ref_rejects_rebound_cog_import() {
        let source = r#"
import cog
import other as cog

class Predictor:
    @cog.concurrent(max=3)
    async def run(self) -> str:
        return "ok"
"#;

        let err = concurrent_max_from_source(source, "Predictor")
            .expect_err("rebound cog import should be rejected");

        assert!(
            err.to_string()
                .contains("concurrent decorator is not imported from cog"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn concurrent_max_from_ref_rejects_kwargs_expansion() {
        let source = r#"
import cog

OPTS = {"max": 3}

class Predictor:
    @cog.concurrent(**OPTS)
    async def run(self) -> str:
        return "ok"
"#;

        let err = concurrent_max_from_source(source, "Predictor")
            .expect_err("kwargs expansion should be rejected");

        assert!(
            err.to_string()
                .contains("concurrent decorator arguments must be literal"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn concurrent_max_from_ref_falls_back_when_runner_direct_and_inherited_conflict() {
        let source = r#"
import cog
from cog import BaseRunner

class PredictMixin:
    async def predict(self) -> str:
        return "predict"

class Runner(PredictMixin, BaseRunner):
    async def run(self) -> str:
        return "run"

class Predictor(BaseRunner):
    @cog.concurrent(max=6)
    async def run(self) -> str:
        return "ok"
"#;

        let concurrent_max = concurrent_max_from_source(source, "")
            .expect("metadata inspection should fall back to Predictor");

        assert_eq!(concurrent_max, Some(6));
    }

    #[test]
    fn concurrent_max_from_ref_rejects_zero_max() {
        let source = r#"
import cog

class Predictor:
    @cog.concurrent(max=0)
    async def run(self) -> str:
        return "ok"
"#;

        let err = concurrent_max_from_source(source, "Predictor")
            .expect_err("zero max should be rejected");

        assert!(
            err.to_string().contains("max must be at least 1"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn concurrent_max_from_ref_rejects_negative_max() {
        let source = r#"
import cog

class Predictor:
    @cog.concurrent(max=-1)
    async def run(self) -> str:
        return "ok"
"#;

        let err = concurrent_max_from_source(source, "Predictor")
            .expect_err("negative max should be rejected");

        assert!(
            err.to_string().contains("max must be at least 1"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn concurrent_max_from_ref_supports_module_constant_max() {
        let source = r#"
import cog
from cog import BaseRunner

MAX_CONCURRENCY = 4

class Predictor(BaseRunner):
    @cog.concurrent(max=MAX_CONCURRENCY)
    async def run(self) -> str:
        return "ok"
"#;

        let concurrent_max = concurrent_max_from_source(source, "Predictor")
            .expect("metadata inspection should resolve module constants");

        assert_eq!(concurrent_max, Some(4));
    }

    #[test]
    fn concurrent_max_from_ref_uses_constant_value_before_decorator() {
        let source = r#"
import cog
from cog import BaseRunner

MAX_CONCURRENCY = 4

class Predictor(BaseRunner):
    @cog.concurrent(max=MAX_CONCURRENCY)
    async def run(self) -> str:
        return "ok"

MAX_CONCURRENCY = 8
"#;

        let concurrent_max = concurrent_max_from_source(source, "Predictor")
            .expect("metadata inspection should use constant value before decorator");

        assert_eq!(concurrent_max, Some(4));
    }

    #[test]
    fn concurrent_max_from_ref_rejects_prior_non_integer_constant_rebind() {
        let source = r#"
import cog
from cog import BaseRunner

MAX_CONCURRENCY = 4
MAX_CONCURRENCY = "bad"

class Predictor(BaseRunner):
    @cog.concurrent(max=MAX_CONCURRENCY)
    async def run(self) -> str:
        return "ok"
"#;

        let err = concurrent_max_from_source(source, "Predictor")
            .expect_err("non-integer constant rebind should be rejected");

        assert!(
            err.to_string()
                .contains("max must be an integer literal or module-level integer constant"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn concurrent_max_from_ref_rejects_prior_annotated_constant_rebind() {
        let source = r#"
import cog
from cog import BaseRunner

MAX_CONCURRENCY = 4
MAX_CONCURRENCY: str = "bad"

class Predictor(BaseRunner):
    @cog.concurrent(max=MAX_CONCURRENCY)
    async def run(self) -> str:
        return "ok"
"#;

        let err = concurrent_max_from_source(source, "Predictor")
            .expect_err("annotated non-integer constant rebind should be rejected");

        assert!(
            err.to_string()
                .contains("max must be an integer literal or module-level integer constant"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn concurrent_max_from_ref_rejects_prior_function_constant_rebind() {
        let source = r#"
import cog
from cog import BaseRunner

MAX_CONCURRENCY = 4

def MAX_CONCURRENCY():
    return 8

class Predictor(BaseRunner):
    @cog.concurrent(max=MAX_CONCURRENCY)
    async def run(self) -> str:
        return "ok"
"#;

        let err = concurrent_max_from_source(source, "Predictor")
            .expect_err("function constant rebind should be rejected");

        assert!(
            err.to_string()
                .contains("max must be an integer literal or module-level integer constant"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn concurrent_max_from_ref_rejects_prior_import_constant_rebind() {
        let source = r#"
import cog
from cog import BaseRunner

MAX_CONCURRENCY = 4
import other as MAX_CONCURRENCY

class Predictor(BaseRunner):
    @cog.concurrent(max=MAX_CONCURRENCY)
    async def run(self) -> str:
        return "ok"
"#;

        let err = concurrent_max_from_source(source, "Predictor")
            .expect_err("import constant rebind should be rejected");

        assert!(
            err.to_string()
                .contains("max must be an integer literal or module-level integer constant"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn concurrent_max_from_ref_rejects_prior_augmented_constant_rebind() {
        let source = r#"
import cog
from cog import BaseRunner

MAX_CONCURRENCY = 4
MAX_CONCURRENCY += 1

class Predictor(BaseRunner):
    @cog.concurrent(max=MAX_CONCURRENCY)
    async def run(self) -> str:
        return "ok"
"#;

        let err = concurrent_max_from_source(source, "Predictor")
            .expect_err("augmented constant rebind should be rejected");

        assert!(
            err.to_string()
                .contains("max must be an integer literal or module-level integer constant"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn concurrent_max_from_ref_rejects_unresolved_max_expression() {
        let source = r#"
import cog

class Predictor:
    @cog.concurrent(max=get_max())
    async def run(self) -> str:
        return "ok"
"#;

        let err = concurrent_max_from_source(source, "Predictor")
            .expect_err("unresolved max expression should be rejected");

        assert!(
            err.to_string()
                .contains("max must be an integer literal or module-level integer constant"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn concurrent_max_from_ref_uses_mixin_before_base_runner() {
        let source = r#"
import cog
from cog import BaseRunner

class RunMixin:
    @cog.concurrent(max=4)
    async def run(self) -> str:
        return "ok"

class Runner(RunMixin, BaseRunner):
    pass
"#;

        let concurrent_max = concurrent_max_from_source(source, "Runner")
            .expect("metadata inspection should inspect same-file mixins");

        assert_eq!(concurrent_max, Some(4));
    }

    #[test]
    fn concurrent_max_from_ref_ignores_mixin_after_base_runner() {
        let source = r#"
import cog
from cog import BaseRunner

class PredictMixin:
    @cog.concurrent(max=4)
    async def predict(self) -> str:
        return "ok"

class Runner(BaseRunner, PredictMixin):
    pass
"#;

        let concurrent_max = concurrent_max_from_source(source, "Runner")
            .expect("metadata inspection should ignore mixins after BaseRunner");

        assert_eq!(concurrent_max, None);
    }

    #[test]
    fn function_run_concurrent_max_loads_from_decorator() {
        let predictor = load_predictor_source_with_ref(
            r#"
import cog

@cog.concurrent(max=4)
async def run() -> str:
    return "ok"
"#,
            "run",
        )
        .expect("function predictor with concurrent decorator should load");

        assert_eq!(predictor.concurrent_max(), Some(4));
    }

    #[test]
    fn undecorated_predictor_has_no_concurrent_max() {
        let predictor = load_predictor_source(
            r#"
from cog import BaseRunner

class Predictor(BaseRunner):
    async def run(self) -> str:
        return "ok"
"#,
        )
        .expect("undecorated predictor should load");

        assert_eq!(predictor.concurrent_max(), None);
    }

    #[test]
    fn class_with_run_and_predict_errors() {
        let err = match load_predictor_source(
            r#"
from cog import BaseRunner

class Predictor(BaseRunner):
    def run(self) -> str:
        return "run"

    def predict(self) -> str:
        return "predict"
"#,
        ) {
            Ok(_) => panic!("predictor with run and predict should error"),
            Err(err) => err,
        };

        let message = err.to_string();
        assert!(message.contains("run"), "unexpected error: {message}");
        assert!(message.contains("predict"), "unexpected error: {message}");
    }

    #[test]
    fn inherited_user_run_loads() {
        let predictor = load_predictor_source(
            r#"
from cog import BaseRunner

class Parent(BaseRunner):
    def run(self) -> str:
        return "ok"

class Predictor(Parent):
    pass
"#,
        )
        .expect("predictor with inherited user run should load");

        assert_eq!(selected_predict_method_name(&predictor), "run");
    }

    #[test]
    fn diamond_inherited_user_run_loads() {
        let predictor = load_predictor_source(
            r#"
from cog import BaseRunner

class Left(BaseRunner):
    pass

class Right(BaseRunner):
    def run(self) -> str:
        return "ok"

class Predictor(Left, Right):
    pass
"#,
        )
        .expect("predictor with diamond-inherited user run should load");

        assert_eq!(selected_predict_method_name(&predictor), "run");
    }

    #[test]
    fn mixin_after_base_runner_is_not_user_predict() {
        let err = match load_predictor_source(
            r#"
from cog import BaseRunner

class PredictMixin:
    def predict(self) -> str:
        return "ok"

class Predictor(BaseRunner, PredictMixin):
    pass
"#,
        ) {
            Ok(_) => panic!("mixin after BaseRunner should not provide selected predict"),
            Err(err) => err,
        };

        let message = err.to_string();
        assert!(message.contains("run"), "unexpected error: {message}");
        assert!(message.contains("predict"), "unexpected error: {message}");
    }

    #[test]
    fn no_user_run_or_predict_errors() {
        let err = match load_predictor_source(
            r#"
from cog import BaseRunner

class Predictor(BaseRunner):
    pass
"#,
        ) {
            Ok(_) => panic!("predictor without run or predict should error"),
            Err(err) => err,
        };

        let message = err.to_string();
        assert!(message.contains("run"), "unexpected error: {message}");
        assert!(message.contains("predict"), "unexpected error: {message}");
    }

    #[test]
    fn legacy_predict_loads_with_fallback() {
        let predictor = load_predictor_source(
            r#"
from cog import BaseRunner

class Predictor(BaseRunner):
    def predict(self) -> str:
        return "ok"
"#,
        )
        .expect("predictor with legacy predict should load");

        assert_eq!(selected_predict_method_name(&predictor), "predict");
    }

    #[test]
    fn legacy_base_predictor_loads() {
        let predictor = load_predictor_source(
            r#"
from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self) -> str:
        return "ok"
"#,
        )
        .expect("predictor with BasePredictor should load");

        assert_eq!(selected_predict_method_name(&predictor), "predict");
    }
}
