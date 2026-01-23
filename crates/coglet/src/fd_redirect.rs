//! File descriptor redirection for subprocess isolation.
//!
//! Worker uses fd 1 (stdout) as the control channel to the orchestrator. When Python's
//! setup() spawns subprocesses (subprocess.Popen), they inherit fd 1 and corrupt the
//! control channel by writing directly into it.
//!
//! We redirect fds early in startup: move control channel to high-numbered fds (99-101),
//! replace fd 1/2 with capture pipes, spawn threads to route captured output through
//! the log system.
//!
//! CRITICAL: Must be called before FFI initialization (Python, etc.).
//!
//! ## Safety contracts
//!
//! All `unsafe` blocks in this module rely on these guarantees:
//! 1. Called early in worker before any predictor/FFI code runs (tokio runtime threads
//!    exist but aren't accessing fds 0/1/2)
//! 2. Standard fds (0, 1, 2) are guaranteed open by the OS at process startup
//! 3. High-numbered fds (99-101) won't conflict with application/library usage
//! 4. Ownership transfer to threads via `from_raw_fd` + `forget` prevents double-close
//!
//! Cannot use Miri: This code makes actual syscalls (dup/dup2) which Miri can't execute.

#[cfg(unix)]
use std::io;
#[cfg(unix)]
use std::os::fd::{AsRawFd, BorrowedFd, FromRawFd, OwnedFd};

#[cfg(unix)]
use nix::unistd::{dup, dup2, pipe};
#[cfg(unix)]
use tokio::sync::mpsc;

#[cfg(unix)]
use crate::bridge::protocol::{ControlResponse, LogSource};

/// Chosen to be above the range typically used by libraries (avoiding conflicts with
/// application fds or library-opened files).
#[cfg(unix)]
const CONTROL_STDIN_FD: i32 = 99;
#[cfg(unix)]
const CONTROL_STDOUT_FD: i32 = 100;
#[cfg(unix)]
const WORKER_STDERR_FD: i32 = 101;

#[cfg(unix)]
pub struct ControlChannelFds {
    pub stdin_fd: OwnedFd,
    pub stdout_fd: OwnedFd,
}

/// Redirect stdout/stderr for subprocess isolation.
///
/// CRITICAL: Must be called before FFI initialization. Child processes spawned after
/// this will inherit the capture pipes (not the control channel).
#[cfg(unix)]
pub fn redirect_fds_for_subprocess_isolation(
    setup_log_tx: mpsc::Sender<ControlResponse>,
) -> io::Result<ControlChannelFds> {
    // Safety: Called early in worker startup before FFI initialization (tokio runtime threads
    // exist but aren't accessing fds 0/1/2). dup/dup2 are atomic. BorrowedFd::borrow_raw is
    // safe because we're borrowing standard fds (0, 1, 2) which are guaranteed to be open.

    tracing::debug!("Preserving control channel to high fds");

    let control_stdin = unsafe {
        let fd = BorrowedFd::borrow_raw(0);
        dup(fd)
    }
    .map_err(|e| io::Error::other(format!("dup(0) failed: {}", e)))?;

    let control_stdout = unsafe {
        let fd = BorrowedFd::borrow_raw(1);
        dup(fd)
    }
    .map_err(|e| io::Error::other(format!("dup(1) failed: {}", e)))?;

    let worker_stderr = unsafe {
        let fd = BorrowedFd::borrow_raw(2);
        dup(fd)
    }
    .map_err(|e| io::Error::other(format!("dup(2) failed: {}", e)))?;

    tracing::trace!(
        control_stdin = control_stdin.as_raw_fd(),
        control_stdout = control_stdout.as_raw_fd(),
        worker_stderr = worker_stderr.as_raw_fd(),
        "Duped original fds"
    );

    let mut target_stdin = unsafe { OwnedFd::from_raw_fd(CONTROL_STDIN_FD) };
    dup2(&control_stdin, &mut target_stdin)
        .map_err(|e| io::Error::other(format!("dup2 stdin failed: {}", e)))?;
    std::mem::forget(target_stdin); // Don't close, we'll use it later

    let mut target_stdout = unsafe { OwnedFd::from_raw_fd(CONTROL_STDOUT_FD) };
    dup2(&control_stdout, &mut target_stdout)
        .map_err(|e| io::Error::other(format!("dup2 stdout failed: {}", e)))?;
    std::mem::forget(target_stdout); // Don't close, we'll use it later

    let mut target_stderr = unsafe { OwnedFd::from_raw_fd(WORKER_STDERR_FD) };
    dup2(&worker_stderr, &mut target_stderr)
        .map_err(|e| io::Error::other(format!("dup2 stderr failed: {}", e)))?;
    std::mem::forget(target_stderr); // Don't close, we'll use it later

    tracing::trace!(
        stdin_fd = CONTROL_STDIN_FD,
        stdout_fd = CONTROL_STDOUT_FD,
        stderr_fd = WORKER_STDERR_FD,
        "Moved control channel to high fds"
    );

    // Temps are now duplicated at high positions, safe to close
    drop(control_stdin);
    drop(control_stdout);
    drop(worker_stderr);

    tracing::debug!("Creating capture pipes for stdout/stderr");

    let (stdout_read, stdout_write) =
        pipe().map_err(|e| io::Error::other(format!("pipe failed: {}", e)))?;
    let (stderr_read, stderr_write) =
        pipe().map_err(|e| io::Error::other(format!("pipe failed: {}", e)))?;

    tracing::trace!(
        stdout_read = stdout_read.as_raw_fd(),
        stdout_write = stdout_write.as_raw_fd(),
        stderr_read = stderr_read.as_raw_fd(),
        stderr_write = stderr_write.as_raw_fd(),
        "Created capture pipes"
    );

    let mut target_fd1 = unsafe { OwnedFd::from_raw_fd(1) };
    dup2(&stdout_write, &mut target_fd1)
        .map_err(|e| io::Error::other(format!("dup2(stdout) failed: {}", e)))?;
    std::mem::forget(target_fd1); // Don't close fd 1

    let mut target_fd2 = unsafe { OwnedFd::from_raw_fd(2) };
    dup2(&stderr_write, &mut target_fd2)
        .map_err(|e| io::Error::other(format!("dup2(stderr) failed: {}", e)))?;
    std::mem::forget(target_fd2); // Don't close fd 2

    tracing::trace!("Replaced fd 1/2 with capture pipes");

    // Write ends are duped to 1/2, close originals
    drop(stdout_write);
    drop(stderr_write);

    tracing::debug!("Spawning capture threads");

    // Capture both stdout and stderr from subprocesses. Rust tracing was initialized before
    // redirection, so its output also flows through the stderr pipe. All captured output
    // routes to coglet::user target. Bounded channel (500 messages) provides backpressure
    // if subprocess output exceeds processing rate.

    let stdout_tx = setup_log_tx.clone();
    let stdout_read_raw = stdout_read.as_raw_fd();
    std::thread::spawn(move || {
        // NOTE: No tracing in capture threads - would create feedback loop (stderr is captured)
        // Safety: We own stdout_read (moved into this thread)
        let mut file = unsafe { std::fs::File::from_raw_fd(stdout_read_raw) };
        let mut buf = [0u8; 4096];

        loop {
            match std::io::Read::read(&mut file, &mut buf) {
                Ok(0) => break,
                Ok(n) => {
                    let data = String::from_utf8_lossy(&buf[..n]).to_string();
                    if stdout_tx
                        .blocking_send(ControlResponse::Log {
                            source: LogSource::Stdout,
                            data,
                        })
                        .is_err()
                    {
                        break;
                    }
                }
                Err(e) if e.kind() == io::ErrorKind::Interrupted => continue,
                Err(_) => break,
            }
        }
    });
    std::mem::forget(stdout_read); // Ownership transferred to thread

    let stderr_tx = setup_log_tx;
    let stderr_read_raw = stderr_read.as_raw_fd();
    std::thread::spawn(move || {
        // NOTE: No tracing in capture threads - would create feedback loop (stderr is captured)
        // Safety: We own stderr_read (moved into this thread)
        let mut file = unsafe { std::fs::File::from_raw_fd(stderr_read_raw) };
        let mut buf = [0u8; 4096];

        loop {
            match std::io::Read::read(&mut file, &mut buf) {
                Ok(0) => break,
                Ok(n) => {
                    let data = String::from_utf8_lossy(&buf[..n]).to_string();
                    if stderr_tx
                        .blocking_send(ControlResponse::Log {
                            source: LogSource::Stderr,
                            data,
                        })
                        .is_err()
                    {
                        break;
                    }
                }
                Err(e) if e.kind() == io::ErrorKind::Interrupted => continue,
                Err(_) => break,
            }
        }
    });
    std::mem::forget(stderr_read); // Ownership transferred to thread

    // Note: Both stdout and stderr now point to capture pipes. Rust tracing was initialized
    // before fd redirection to write to stderr, so its output will be captured along with
    // subprocess stderr. Both will be routed to coglet::user target. The original stderr
    // is still available at fd 101 but unused after redirection.

    tracing::info!("File descriptor redirection complete");

    // Safety: We own these fds
    Ok(ControlChannelFds {
        stdin_fd: unsafe { OwnedFd::from_raw_fd(CONTROL_STDIN_FD) },
        stdout_fd: unsafe { OwnedFd::from_raw_fd(CONTROL_STDOUT_FD) },
    })
}

#[cfg(not(unix))]
pub struct ControlChannelFds {
    pub stdin_fd: std::io::Stdin,
    pub stdout_fd: std::io::Stdout,
}

#[cfg(not(unix))]
pub fn redirect_fds_for_subprocess_isolation(
    _setup_log_tx: tokio::sync::mpsc::Sender<crate::bridge::protocol::ControlResponse>,
) -> io::Result<ControlChannelFds> {
    // No fd redirection on non-Unix - subprocesses will pollute control channel
    Ok(ControlChannelFds {
        stdin_fd: std::io::stdin(),
        stdout_fd: std::io::stdout(),
    })
}
