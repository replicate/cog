//! Generate Python stub files for coglet.
//!
//! Run with: cargo run --bin stub_gen

use pyo3_stub_gen::Result;

fn main() -> Result<()> {
    let stub = coglet::stub_info()?;
    stub.generate()?;
    Ok(())
}
