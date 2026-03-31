-- Multimodal asset support: track uploaded/ingested assets (PDFs, images, audio, URLs)
-- and per-modality embedding model configuration.

-- ── repo_assets: tracks every non-code asset ingested into a repo's index ──────────

CREATE TABLE IF NOT EXISTS repo_assets (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id       UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    asset_type    TEXT NOT NULL CHECK (asset_type IN ('pdf', 'image', 'audio', 'video', 'webpage', 'document')),
    source_url    TEXT,
    file_name     TEXT,
    mime_type     TEXT,
    size_bytes    BIGINT DEFAULT 0,
    chunks_count  INT DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'ready', 'error')),
    error_message TEXT,
    metadata      JSONB DEFAULT '{}',
    created_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_repo_assets_repo        ON repo_assets(repo_id);
CREATE INDEX IF NOT EXISTS idx_repo_assets_repo_type   ON repo_assets(repo_id, asset_type);
CREATE INDEX IF NOT EXISTS idx_repo_assets_status      ON repo_assets(repo_id, status);

-- ── embedding_model_config: per-modality model selection ────────────────────────────
-- Each user can configure independent embedding models for text, image, and audio.
-- Defaults are applied at the application layer when no row exists.

CREATE TABLE IF NOT EXISTS embedding_model_config (
    id            SERIAL PRIMARY KEY,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    modality      TEXT NOT NULL CHECK (modality IN ('text', 'image', 'audio')),
    model_name    TEXT NOT NULL,
    backend       TEXT NOT NULL DEFAULT 'onnx' CHECK (backend IN ('onnx', 'http', 'mock')),
    dimension     INT NOT NULL,
    enabled       BOOLEAN NOT NULL DEFAULT true,
    config_json   JSONB DEFAULT '{}',
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, modality)
);

-- ── model_download_status: track on-demand model downloads ──────────────────────────

CREATE TABLE IF NOT EXISTS model_download_status (
    model_name    TEXT PRIMARY KEY,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'downloading', 'ready', 'error')),
    progress      INT DEFAULT 0,           -- 0-100
    size_bytes    BIGINT DEFAULT 0,
    error_message TEXT,
    started_at    TIMESTAMPTZ,
    completed_at  TIMESTAMPTZ,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed default download statuses for builtin text models (always ready).
INSERT INTO model_download_status (model_name, status, progress, completed_at)
VALUES
    ('bge-m3',     'ready', 100, NOW()),
    ('minilm',     'ready', 100, NOW()),
    ('siglip-base','pending', 0, NULL),
    ('clap-general','pending', 0, NULL)
ON CONFLICT (model_name) DO NOTHING;
