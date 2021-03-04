CREATE TABLE models (
    model_id            SERIAL PRIMARY KEY,
    name                VARCHAR(128),
    hash                CHAR(40),
    gcs_path            TEXT,
    cpu_image        TEXT,
    gpu_image        TEXT
);
