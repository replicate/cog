use std::path::PathBuf;
use std::process;

use schema_gen::error::SchemaError;
use schema_gen::types::Mode;

fn main() {
    let args: Vec<String> = std::env::args().collect();

    let (predict_ref, mode_str, src) = match parse_args(&args) {
        Ok(v) => v,
        Err(msg) => {
            eprintln!("error: {msg}");
            eprintln!();
            eprintln!("Usage: cog-schema-gen <predict_ref> [--mode predict|train] [--src <dir>]");
            eprintln!();
            eprintln!("Arguments:");
            eprintln!(
                "  <predict_ref>    Predictor reference: file.py:ClassName or file.py:function_name"
            );
            eprintln!();
            eprintln!("Options:");
            eprintln!("  --mode <mode>    Mode: predict or train [default: predict]");
            eprintln!("  --src <dir>      Source directory [default: .]");
            process::exit(2);
        }
    };

    if let Err(e) = run(&predict_ref, &mode_str, &src) {
        eprintln!("error: {e}");
        process::exit(1);
    }
}

fn parse_args(args: &[String]) -> Result<(String, String, PathBuf), String> {
    let mut predict_ref: Option<String> = None;
    let mut mode = "predict".to_string();
    let mut src = PathBuf::from(".");

    let mut i = 1; // skip argv[0]
    while i < args.len() {
        match args[i].as_str() {
            "--mode" => {
                i += 1;
                mode = args.get(i).ok_or("--mode requires a value")?.clone();
            }
            "--src" => {
                i += 1;
                src = PathBuf::from(args.get(i).ok_or("--src requires a value")?);
            }
            "--help" | "-h" => return Err("".to_string()),
            arg if arg.starts_with('-') => return Err(format!("unknown flag: {arg}")),
            arg => {
                if predict_ref.is_some() {
                    return Err(format!("unexpected argument: {arg}"));
                }
                predict_ref = Some(arg.to_string());
            }
        }
        i += 1;
    }

    let predict_ref = predict_ref.ok_or("missing required argument: <predict_ref>")?;
    Ok((predict_ref, mode, src))
}

fn run(predict_ref: &str, mode_str: &str, src: &std::path::Path) -> Result<(), SchemaError> {
    let mode = match mode_str {
        "predict" => Mode::Predict,
        "train" => Mode::Train,
        other => {
            return Err(SchemaError::Other(format!(
                "invalid mode '{other}', expected 'predict' or 'train'"
            )));
        }
    };

    // Parse the predict ref: "predict.py:Predictor" → (file, class/function)
    let (file_part, ref_name) = predict_ref
        .rsplit_once(':')
        .ok_or_else(|| SchemaError::InvalidPredictRef(predict_ref.to_string()))?;

    let file_path = src.join(file_part);

    if !file_path.exists() {
        return Err(SchemaError::FileNotFound(file_path.display().to_string()));
    }

    let source = std::fs::read_to_string(&file_path)
        .map_err(|e| SchemaError::Other(format!("failed to read {}: {e}", file_path.display())))?;

    let schema = schema_gen::generate_schema(&source, ref_name, mode)?;

    let json = serde_json::to_string_pretty(&schema)
        .map_err(|e| SchemaError::Other(format!("JSON serialization failed: {e}")))?;

    println!("{json}");

    Ok(())
}
