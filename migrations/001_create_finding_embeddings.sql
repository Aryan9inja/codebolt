CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS finding_embeddings (
    id          UUID        PRIMARY KEY,
    repo        TEXT        NOT NULL,
    file_path   TEXT        NOT NULL,
    rule        TEXT        NOT NULL,
    message     TEXT        NOT NULL,
    embedding   VECTOR(768) NOT NULL,
    pr_number   INT         NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- repo filter runs before the vector scan, so this index matters
CREATE INDEX IF NOT EXISTS finding_embeddings_repo_idx
    ON finding_embeddings (repo);

-- HNSW over IVFFlat: no cold-start clustering problem, builds
-- incrementally as rows are inserted — right call for a table
-- that starts at zero and grows slowly
CREATE INDEX IF NOT EXISTS finding_embeddings_embedding_idx
    ON finding_embeddings USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);