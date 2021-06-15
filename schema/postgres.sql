CREATE TABLE versions (
    id          TEXT            NOT NULL,
    username    TEXT            NOT NULL,
    model_name  TEXT            NOT NULL,
    data        JSON            NOT NULL,
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, username, model_name)
);

CREATE TABLE images (
    version_id  TEXT            NOT NULL,
    username    TEXT            NOT NULL,
    model_name  TEXT            NOT NULL,
    arch        TEXT            NOT NULL,
    data        JSON            NOT NULL,
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    PRIMARY KEY (version_id, username, model_name, arch)
);

CREATE TABLE build_log_lines (
    id          SERIAL          NOT NULL PRIMARY KEY,
    username    TEXT            NOT NULL,
    model_name  TEXT            NOT NULL,
    build_id    TEXT            NOT NULL,
    level       INT             NOT NULL DEFAULT 0,
    line        TEXT            NOT NULL DEFAULT '',
    done        BOOL            NOT NULL DEFAULT FALSE,
    timestamp_nano BIGINT       NOT NULL
);
