//! Rust-owned async runtime for predictions.
//!
//! Handles async predictor execution with:
//! - Task tracking by SlotId (for cancellation)
//! - ContextVar setup for log routing (by prediction_id)
//! - Proper cancellation handling (uncancel for cleanup)
//!
//! User code provides coroutines. We schedule them. They don't control the loop.

use std::collections::HashMap;
use std::sync::{Mutex, OnceLock};

use pyo3::prelude::*;
use pyo3::types::PyDict;

use coglet_worker::SlotId;

/// Registry of active async tasks by SlotId.
/// Rust owns this - user code cannot access it.
/// Used for cancellation lookup.
static TASK_REGISTRY: OnceLock<Mutex<HashMap<SlotId, Py<PyAny>>>> = OnceLock::new();

fn get_task_registry() -> &'static Mutex<HashMap<SlotId, Py<PyAny>>> {
    TASK_REGISTRY.get_or_init(|| Mutex::new(HashMap::new()))
}

/// Register a task for a slot.
pub fn register_task(slot_id: SlotId, task: Py<PyAny>) {
    let mut registry = get_task_registry().lock().unwrap();
    registry.insert(slot_id, task);
}

/// Unregister a task for a slot.
pub fn unregister_task(slot_id: SlotId) {
    let mut registry = get_task_registry().lock().unwrap();
    registry.remove(&slot_id);
}

/// Get a task by slot ID.
pub fn get_task(py: Python<'_>, slot_id: SlotId) -> Option<Py<PyAny>> {
    let registry = get_task_registry().lock().unwrap();
    registry.get(&slot_id).map(|t| t.clone_ref(py))
}

/// Cancel an async prediction by SlotId.
///
/// Looks up the asyncio.Task and calls task.cancel().
/// Returns Ok(true) if cancelled, Ok(false) if no task found.
pub fn cancel_task(py: Python<'_>, slot_id: SlotId) -> PyResult<bool> {
    let task = match get_task(py, slot_id) {
        Some(t) => t,
        None => {
            tracing::debug!(%slot_id, "No task found to cancel");
            return Ok(false);
        }
    };

    // Call task.cancel()
    task.call_method0(py, "cancel")?;
    tracing::debug!(%slot_id, "Cancelled async task");
    Ok(true)
}

/// Create the async wrapper code that:
/// 1. Sets up ContextVar for log routing (prediction_id)
/// 2. Runs the user's coroutine
/// 3. Handles cancellation properly (uncancel for cleanup)
/// 4. Returns result or error
///
/// The wrapper receives the prediction_id and sets the _coglet_prediction_id ContextVar.
/// This propagates to all subtasks automatically.
static WRAPPER_CODE: &str = r#"
import asyncio
import contextvars

# Get the _coglet_prediction_id ContextVar (created by Rust)
# We look it up by name from the current context
def _get_prediction_contextvar():
    for var in contextvars.copy_context():
        if var.name == "_coglet_prediction_id":
            return var
    # Create if not found (shouldn't happen, but defensive)
    return contextvars.ContextVar("_coglet_prediction_id")

_COGLET_PREDICTION_ID = _get_prediction_contextvar()

async def _coglet_run_prediction(coro, prediction_id):
    """
    Wrapper that runs a prediction coroutine with proper context management.
    
    Sets _coglet_prediction_id ContextVar before running user's coroutine.
    This is called by Rust. User code cannot call this directly.
    """
    # Set up log routing context - this propagates to all subtasks
    token = _COGLET_PREDICTION_ID.set(prediction_id)
    
    try:
        # Run the user's coroutine
        result = await coro
        return ("ok", result)
    except asyncio.CancelledError:
        # Handle cancellation - uncancel so cleanup can proceed
        task = asyncio.current_task()
        if task:
            task.uncancel()
        return ("cancelled", None)
    except Exception as e:
        return ("error", str(e))
    finally:
        # Reset context (optional - task ending cleans up anyway)
        _COGLET_PREDICTION_ID.reset(token)
"#;

/// Get or create the wrapper function.
fn get_wrapper_fn(py: Python<'_>) -> PyResult<Py<PyAny>> {
    static WRAPPER_FN: OnceLock<Py<PyAny>> = OnceLock::new();

    if let Some(fn_obj) = WRAPPER_FN.get() {
        return Ok(fn_obj.clone_ref(py));
    }

    // Ensure the ContextVar exists before running wrapper code
    // This creates it if needed, so the wrapper can find it
    let contextvars = py.import("contextvars")?;
    let _ = contextvars.call_method1("ContextVar", ("_coglet_prediction_id",))?;

    // Execute the wrapper code to define the function
    let globals = PyDict::new(py);
    let builtins = py.import("builtins")?;
    let exec_fn = builtins.getattr("exec")?;
    exec_fn.call1((WRAPPER_CODE, &globals))?;

    let wrapper_fn = globals
        .get_item("_coglet_run_prediction")?
        .ok_or_else(|| {
            pyo3::exceptions::PyRuntimeError::new_err("Failed to create wrapper function")
        })?
        .unbind();

    // Store it (race is fine)
    let _ = WRAPPER_FN.set(wrapper_fn.clone_ref(py));
    Ok(wrapper_fn)
}

/// Wrap a user's coroutine with our context management.
///
/// Returns a new coroutine that:
/// - Sets cog_prediction_id ContextVar before running
/// - Handles CancelledError properly
/// - Resets ContextVar after completion (optional cleanup)
pub fn wrap_prediction_coro(
    py: Python<'_>,
    coro: &Bound<'_, PyAny>,
    prediction_id: &str,
) -> PyResult<Py<PyAny>> {
    let wrapper_fn = get_wrapper_fn(py)?;

    // Call wrapper to create wrapped coroutine
    let wrapped = wrapper_fn.call1(py, (coro, prediction_id))?;

    Ok(wrapped)
}

/// Create an asyncio.Task for a prediction.
///
/// The task is tracked in our registry by SlotId (for cancellation).
/// The ContextVar is set with prediction_id (for log routing).
/// Returns the task object (for internal use only).
pub fn create_prediction_task(
    py: Python<'_>,
    coro: &Bound<'_, PyAny>,
    slot_id: SlotId,
    prediction_id: &str,
) -> PyResult<Py<PyAny>> {
    let asyncio = py.import("asyncio")?;

    // Wrap the coroutine with our context management
    let wrapped_coro = wrap_prediction_coro(py, coro, prediction_id)?;

    // Create task using asyncio.create_task()
    let task = asyncio.call_method1("create_task", (wrapped_coro,))?;

    // Register in our task registry (keyed by slot for cancellation)
    register_task(slot_id, task.clone().unbind());

    tracing::debug!(%slot_id, %prediction_id, "Created async prediction task");
    Ok(task.unbind())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn task_registry_operations() {
        let slot_id = SlotId::new();

        // Initially no task in registry
        let registry = get_task_registry().lock().unwrap();
        assert!(!registry.contains_key(&slot_id));

        // Would need Python to create actual task objects for full test
    }
}
