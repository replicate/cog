//! Metric scope: type-safe metric recording with ContextVar routing.
//!
//! Two PyO3 classes:
//! - `Scope` — the per-prediction context, obtained via `current_scope()`
//! - `MetricRecorder` — the `scope.metrics` sub-object with type invariant
//!   enforcement, dict-style access, and accumulation modes
//!
//! All validation happens in Rust (PyO3, in-process). IPC sends the validated
//! metric to the coglet server via SlotSender.

use std::collections::HashMap;
use std::sync::{Arc, Mutex, OnceLock};

use pyo3::exceptions::PyTypeError;
use pyo3::prelude::*;
use pyo3_stub_gen::derive::*;

use coglet_core::bridge::protocol::MetricMode;
use coglet_core::worker::SlotSender;

// ============================================================================
// Value type tracking for type invariant
// ============================================================================

/// Coarse type tag for enforcing the type invariant.
/// Once a key is set with a type, it cannot be changed without deleting first.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum MetricValueType {
    Bool,
    Int,
    Float,
    Str,
    List,
    Dict,
}

impl MetricValueType {
    /// Classify a Python object into a type tag.
    fn from_py(obj: &Bound<'_, PyAny>) -> PyResult<Self> {
        // Order matters: bool before int (bool is a subclass of int in Python)
        if obj.is_instance_of::<pyo3::types::PyBool>() {
            Ok(Self::Bool)
        } else if obj.is_instance_of::<pyo3::types::PyInt>() {
            Ok(Self::Int)
        } else if obj.is_instance_of::<pyo3::types::PyFloat>() {
            Ok(Self::Float)
        } else if obj.is_instance_of::<pyo3::types::PyString>() {
            Ok(Self::Str)
        } else if obj.is_instance_of::<pyo3::types::PyList>() {
            Ok(Self::List)
        } else if obj.is_instance_of::<pyo3::types::PyDict>() {
            Ok(Self::Dict)
        } else {
            let type_name = obj.get_type().name()?.to_string();
            Err(PyTypeError::new_err(format!(
                "Unsupported metric value type: {}. Expected bool, int, float, str, list, or dict.",
                type_name
            )))
        }
    }

    fn as_str(&self) -> &'static str {
        match self {
            Self::Bool => "bool",
            Self::Int => "int",
            Self::Float => "float",
            Self::Str => "str",
            Self::List => "list",
            Self::Dict => "dict",
        }
    }
}

// ============================================================================
// MetricRecorder — scope.metrics sub-object
// ============================================================================

/// Metric recorder with type invariant enforcement.
///
/// Accessed via `scope.metrics`. Supports:
/// - `scope.metrics.record(key, value, mode="replace")` — full API
/// - `scope.metrics.delete(key)` — delete (required before type change)
/// - `scope.metrics[key] = value` — dict-style set (replace mode)
/// - `del scope.metrics[key]` — dict-style delete
#[gen_stub_pyclass]
#[pyclass(name = "MetricRecorder", module = "coglet._sdk")]
pub struct MetricRecorder {
    inner: Mutex<Option<RecorderInner>>,
}

struct RecorderInner {
    /// Type tag per metric key — enforces type invariant.
    types: HashMap<String, MetricValueType>,
    /// IPC sender to the coglet server.
    sender: Arc<SlotSender>,
}

impl MetricRecorder {
    pub fn new(sender: Arc<SlotSender>) -> Self {
        Self {
            inner: Mutex::new(Some(RecorderInner {
                types: HashMap::new(),
                sender,
            })),
        }
    }

    pub fn noop() -> Self {
        Self {
            inner: Mutex::new(None),
        }
    }
}

#[gen_stub_pymethods]
#[pymethods]
impl MetricRecorder {
    /// Record a metric value.
    ///
    /// Args:
    ///     key: Metric name. Dot-separated keys (e.g. "timing.preprocess") create
    ///         nested objects in the response.
    ///     value: Must be bool, int, float, str, list, or dict. Once a key is set
    ///         with a type, it cannot be changed without calling delete() first.
    ///     mode: Accumulation mode — "replace" (default), "incr" (increment numeric),
    ///         or "append" (push to array).
    #[pyo3(signature = (key, value, mode=None))]
    fn record(
        &self,
        py: Python<'_>,
        key: &str,
        value: &Bound<'_, PyAny>,
        mode: Option<&str>,
    ) -> PyResult<()> {
        let mode = parse_mode(mode)?;

        let mut guard = self.inner.lock().expect("metric_recorder mutex poisoned");
        let Some(inner) = guard.as_mut() else {
            return Ok(()); // no-op outside prediction
        };

        record_impl(py, inner, key, value, mode)
    }

    /// Delete a metric key. Required before changing a metric's type.
    fn delete(&self, key: &str) -> PyResult<()> {
        let mut guard = self.inner.lock().expect("metric_recorder mutex poisoned");
        let Some(inner) = guard.as_mut() else {
            return Ok(());
        };

        delete_impl(inner, key)
    }

    /// Dict-style set: `scope.metrics["key"] = value`
    fn __setitem__(&self, py: Python<'_>, key: &str, value: &Bound<'_, PyAny>) -> PyResult<()> {
        if value.is_none() {
            return self.delete(key);
        }

        let mut guard = self.inner.lock().expect("metric_recorder mutex poisoned");
        let Some(inner) = guard.as_mut() else {
            return Ok(());
        };

        record_impl(py, inner, key, value, MetricMode::Replace)
    }

    /// Dict-style delete: `del scope.metrics["key"]`
    fn __delitem__(&self, key: &str) -> PyResult<()> {
        self.delete(key)
    }

    fn __repr__(&self) -> String {
        let guard = self.inner.lock().expect("metric_recorder mutex poisoned");
        match guard.as_ref() {
            Some(inner) => format!("MetricRecorder(keys={})", inner.types.len()),
            None => "MetricRecorder(inactive)".to_string(),
        }
    }
}

// ============================================================================
// Scope — the per-prediction context
// ============================================================================

/// Prediction scope, obtained via `current_scope()`.
///
/// Provides access to `scope.metrics` for recording metrics, and
/// `scope.record_metric()` as a convenience shorthand.
#[gen_stub_pyclass]
#[pyclass(name = "Scope", module = "coglet._sdk")]
pub struct Scope {
    metrics_recorder: Py<MetricRecorder>,
}

impl Scope {
    pub fn new(py: Python<'_>, sender: Arc<SlotSender>) -> PyResult<Self> {
        let recorder = Py::new(py, MetricRecorder::new(sender))?;
        Ok(Self {
            metrics_recorder: recorder,
        })
    }

    pub fn noop(py: Python<'_>) -> PyResult<Self> {
        let recorder = Py::new(py, MetricRecorder::noop())?;
        Ok(Self {
            metrics_recorder: recorder,
        })
    }
}

#[gen_stub_pymethods]
#[pymethods]
impl Scope {
    /// The metric recorder for this prediction.
    #[getter]
    fn metrics(&self, py: Python<'_>) -> Py<MetricRecorder> {
        self.metrics_recorder.clone_ref(py)
    }

    /// Convenience: record a metric value.
    ///
    /// Equivalent to `scope.metrics.record(key, value, mode)`.
    #[pyo3(signature = (key, value, mode=None))]
    fn record_metric(
        &self,
        py: Python<'_>,
        key: &str,
        value: &Bound<'_, PyAny>,
        mode: Option<&str>,
    ) -> PyResult<()> {
        self.metrics_recorder
            .borrow(py)
            .record(py, key, value, mode)
    }

    fn __repr__(&self, py: Python<'_>) -> String {
        let recorder = self.metrics_recorder.borrow(py);
        format!("Scope({})", recorder.__repr__())
    }
}

// ============================================================================
// Shared implementation
// ============================================================================

fn parse_mode(mode: Option<&str>) -> PyResult<MetricMode> {
    match mode {
        None | Some("replace") => Ok(MetricMode::Replace),
        Some("incr") | Some("increment") => Ok(MetricMode::Increment),
        Some("append") => Ok(MetricMode::Append),
        Some(other) => Err(PyTypeError::new_err(format!(
            "Invalid metric mode: '{}'. Expected 'replace', 'incr', or 'append'.",
            other
        ))),
    }
}

fn record_impl(
    _py: Python<'_>,
    inner: &mut RecorderInner,
    key: &str,
    value: &Bound<'_, PyAny>,
    mode: MetricMode,
) -> PyResult<()> {
    let value_type = MetricValueType::from_py(value)?;

    // Type invariant check
    if let Some(existing_type) = inner.types.get(key)
        && *existing_type != value_type
    {
        return Err(PyTypeError::new_err(format!(
            "Metric '{}' has type {}, cannot set to {} without deleting first",
            key,
            existing_type.as_str(),
            value_type.as_str(),
        )));
    }

    // Mode-specific validation
    if mode == MetricMode::Increment
        && !matches!(value_type, MetricValueType::Int | MetricValueType::Float)
    {
        return Err(PyTypeError::new_err(format!(
            "Increment mode requires int or float, got {}",
            value_type.as_str()
        )));
    }

    let json_value = py_to_json(value)?;

    inner.types.insert(key.to_string(), value_type);

    inner
        .sender
        .send_metric(key.to_string(), json_value, mode)
        .map_err(|e| pyo3::exceptions::PyIOError::new_err(format!("Failed to send metric: {}", e)))
}

fn delete_impl(inner: &mut RecorderInner, key: &str) -> PyResult<()> {
    inner.types.remove(key);
    inner
        .sender
        .send_metric(
            key.to_string(),
            serde_json::Value::Null,
            MetricMode::Replace,
        )
        .map_err(|e| {
            pyo3::exceptions::PyIOError::new_err(format!("Failed to send metric delete: {}", e))
        })
}

// ============================================================================
// ContextVar-based routing (same pattern as log_writer.rs)
// ============================================================================

/// Global ContextVar for the current Scope.
static SCOPE_CONTEXTVAR: OnceLock<Py<PyAny>> = OnceLock::new();

/// Current sync scope (for sync predictions where ContextVar doesn't work across attach calls).
static SYNC_SCOPE: OnceLock<Mutex<Option<Py<Scope>>>> = OnceLock::new();

fn get_sync_scope_slot() -> &'static Mutex<Option<Py<Scope>>> {
    SYNC_SCOPE.get_or_init(|| Mutex::new(None))
}

fn get_scope_contextvar(py: Python<'_>) -> PyResult<&'static Py<PyAny>> {
    if let Some(cv) = SCOPE_CONTEXTVAR.get() {
        return Ok(cv);
    }

    let contextvars = py.import("contextvars")?;
    let cv = contextvars.call_method1("ContextVar", ("_coglet_metric_scope",))?;

    match SCOPE_CONTEXTVAR.set(cv.unbind()) {
        Ok(()) => {}
        Err(_already_set) => {}
    }

    SCOPE_CONTEXTVAR.get().ok_or_else(|| {
        pyo3::exceptions::PyRuntimeError::new_err("Failed to initialize scope ContextVar")
    })
}

/// Set the current scope in the ContextVar (for async predictions).
pub fn set_current_scope(py: Python<'_>, scope: &Py<Scope>) -> PyResult<Py<PyAny>> {
    let cv = get_scope_contextvar(py)?;
    let token = cv.call_method1(py, "set", (scope,))?;
    Ok(token)
}

/// Set the current sync scope (for sync predictions).
pub fn set_sync_scope(py: Python<'_>, scope: Option<&Py<Scope>>) {
    let mut slot = get_sync_scope_slot()
        .lock()
        .expect("sync_scope mutex poisoned");
    *slot = scope.map(|s| s.clone_ref(py));
}

/// Clear the sync scope.
pub fn clear_sync_scope() {
    let mut slot = get_sync_scope_slot()
        .lock()
        .expect("sync_scope mutex poisoned");
    *slot = None;
}

/// Python-callable: get the current Scope.
///
/// Returns the active scope if inside a prediction, or a no-op scope otherwise.
#[gen_stub_pyfunction(module = "coglet._sdk")]
#[pyfunction]
#[pyo3(name = "current_scope")]
pub fn py_current_scope(py: Python<'_>) -> PyResult<Py<Scope>> {
    // Try sync scope first
    {
        let slot = get_sync_scope_slot()
            .lock()
            .expect("sync_scope mutex poisoned");
        if let Some(ref scope) = *slot {
            return Ok(scope.clone_ref(py));
        }
    }

    // Try ContextVar (async predictions)
    if let Some(cv) = SCOPE_CONTEXTVAR.get() {
        match cv.call_method0(py, "get") {
            Ok(val) => {
                let scope: Py<Scope> = val.extract(py)?;
                return Ok(scope);
            }
            Err(e) if e.is_instance_of::<pyo3::exceptions::PyLookupError>(py) => {
                // Not set — fall through to no-op
            }
            Err(e) => return Err(e),
        }
    }

    // Outside prediction context — return no-op scope
    Py::new(py, Scope::noop(py)?)
}

// ============================================================================
// RAII guard for prediction scope lifecycle
// ============================================================================

/// RAII guard that manages the Scope for a prediction.
///
/// On creation, creates a Scope with a MetricRecorder and sets it in
/// ContextVar + sync scope. On drop, clears the scope and releases the
/// Arc<SlotSender> so the log-forwarder channel can close.
pub struct ScopeGuard {
    scope: Py<Scope>,
    #[allow(dead_code)]
    token: Py<PyAny>,
}

impl ScopeGuard {
    /// Enter scope for a prediction.
    pub fn enter(py: Python<'_>, sender: Arc<SlotSender>) -> PyResult<Self> {
        let scope = Py::new(py, Scope::new(py, sender)?)?;

        let token = set_current_scope(py, &scope)?;
        set_sync_scope(py, Some(&scope));

        Ok(Self { scope, token })
    }
}

impl Drop for ScopeGuard {
    fn drop(&mut self) {
        clear_sync_scope();

        // Acquire the GIL to release the Arc<SlotSender> held by the MetricRecorder.
        // Without this, the Py<Scope> destructor may not run immediately (PyO3
        // defers ref-count decrements when the GIL is not held), keeping the
        // SlotSender channel alive and blocking the log-forwarder shutdown.
        Python::attach(|py| {
            let scope = self.scope.borrow(py);
            let recorder = scope.metrics_recorder.borrow(py);
            let mut guard = recorder
                .inner
                .lock()
                .expect("metric_recorder mutex poisoned");
            // Drop the RecorderInner (and its Arc<SlotSender>)
            *guard = None;
        });
    }
}

// ============================================================================
// Python → JSON conversion
// ============================================================================

fn py_to_json(obj: &Bound<'_, PyAny>) -> PyResult<serde_json::Value> {
    if obj.is_none() {
        Ok(serde_json::Value::Null)
    } else if obj.is_instance_of::<pyo3::types::PyBool>() {
        Ok(serde_json::Value::Bool(obj.extract::<bool>()?))
    } else if obj.is_instance_of::<pyo3::types::PyInt>() {
        if let Ok(v) = obj.extract::<i64>() {
            Ok(serde_json::json!(v))
        } else {
            Ok(serde_json::json!(obj.extract::<f64>()?))
        }
    } else if obj.is_instance_of::<pyo3::types::PyFloat>() {
        Ok(serde_json::json!(obj.extract::<f64>()?))
    } else if obj.is_instance_of::<pyo3::types::PyString>() {
        Ok(serde_json::Value::String(obj.extract::<String>()?))
    } else if obj.is_instance_of::<pyo3::types::PyList>() {
        let list = obj.cast::<pyo3::types::PyList>()?;
        let items: Vec<serde_json::Value> = list
            .iter()
            .map(|item| py_to_json(&item))
            .collect::<PyResult<_>>()?;
        Ok(serde_json::Value::Array(items))
    } else if obj.is_instance_of::<pyo3::types::PyDict>() {
        let dict = obj.cast::<pyo3::types::PyDict>()?;
        let mut map = serde_json::Map::new();
        for (k, v) in dict.iter() {
            let key: String = k.extract()?;
            map.insert(key, py_to_json(&v)?);
        }
        Ok(serde_json::Value::Object(map))
    } else {
        let type_name = obj.get_type().name()?.to_string();
        Err(PyTypeError::new_err(format!(
            "Cannot convert {} to JSON metric value",
            type_name
        )))
    }
}
