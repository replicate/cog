use std::process::ExitCode;

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    match args.as_slice() {
        [flag] if flag == "--version" || flag == "-V" => {
            println!("coglet {}", coglet::COGLET_VERSION);
            ExitCode::SUCCESS
        }
        _ => {
            eprintln!("usage: coglet --version");
            ExitCode::from(2)
        }
    }
}
