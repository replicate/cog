//! Generate Python stub files for coglet.
//!
//! Run with: cargo run --bin stub_gen
//!
//! Custom generate logic: pyo3-stub-gen places classes from the native
//! `coglet._impl` module into the `coglet` parent package, but mypy stubgen
//! overwrites `coglet/__init__.pyi` from the hand-maintained `__init__.py`.
//! We redirect the `coglet` module output to `coglet/_impl.pyi` so the
//! native module types are preserved.

use pyo3_stub_gen::Result;
use std::fs;
use std::io::Write;

fn main() -> Result<()> {
    let stub = coglet::stub_info()?;

    for (name, module) in &stub.modules {
        let normalized = name.replace('-', "_");

        let dest = if normalized == "coglet" {
            // Native module classes land here — redirect to _impl.pyi
            stub.python_root.join("coglet").join("_impl.pyi")
        } else {
            // Submodules like "coglet._sdk" → coglet/_sdk/__init__.pyi
            let path = normalized.replace('.', "/");
            stub.python_root.join(&path).join("__init__.pyi")
        };

        let dir = dest.parent().expect("cannot get parent directory");
        if !dir.exists() {
            fs::create_dir_all(dir)?;
        }

        let mut f = fs::File::create(&dest)?;
        write!(f, "{module}")?;
        eprintln!("Generated stub: {}", dest.display());
    }

    Ok(())
}
