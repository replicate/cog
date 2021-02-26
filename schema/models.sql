CREATE TABLE models (
    model_id            SERIAL PRIMARY KEY,
    name                VARCHAR(128),
    hash                CHAR(20),
    gcs_path            VARCHAR(1000),
    docker_image        VARCHAR(1000)
);
