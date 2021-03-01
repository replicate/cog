CREATE TABLE models (
    model_id            SERIAL PRIMARY KEY,
    name                VARCHAR(128),
    hash                CHAR(40),
    gcs_path            TEXT,
    docker_image        TEXT
);
