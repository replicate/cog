//! HTTP transport for coglet using axum.

mod routes;
mod server;

pub use server::{ServerConfig, serve};
