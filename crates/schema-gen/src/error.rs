use thiserror::Error;

#[derive(Debug, Error)]
pub enum SchemaError {
    #[error("failed to parse Python source: {0}")]
    ParseError(String),

    #[error("predictor not found: {0}")]
    PredictorNotFound(String),

    #[error("predict/train method not found on {0}")]
    MethodNotFound(String),

    #[error("missing return type annotation on {method}")]
    MissingReturnType { method: String },

    #[error("missing type annotation for parameter '{param}' on {method}")]
    MissingTypeAnnotation { method: String, param: String },

    #[error("unsupported type: {0}")]
    UnsupportedType(String),

    #[error(
        "default_factory is not supported in Input() — use a literal default value instead (parameter '{param}')"
    )]
    DefaultFactoryNotSupported { param: String },

    #[error("invalid constraint on parameter '{param}': {reason}")]
    InvalidConstraint { param: String, reason: String },

    #[error(
        "invalid predict reference '{0}' — expected format: file.py:ClassName or file.py:function_name"
    )]
    InvalidPredictRef(String),

    #[error("file not found: {0}")]
    FileNotFound(String),

    #[error("unsupported output type: Optional is not allowed as a return type")]
    OptionalOutput,

    #[error("ConcatenateIterator element type must be str, got {0}")]
    ConcatIteratorNotStr(String),

    #[error("{0}")]
    Other(String),
}

pub type Result<T> = std::result::Result<T, SchemaError>;
