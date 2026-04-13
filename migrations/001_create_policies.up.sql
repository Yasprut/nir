CREATE TABLE IF NOT EXISTS policies (
    id                BIGSERIAL       PRIMARY KEY,
    policy_id         TEXT            NOT NULL UNIQUE,
    type              TEXT            NOT NULL DEFAULT 'baseline'
                      CHECK (type IN ('baseline', 'augment', 'override', 'restrict')),
    priority          INTEGER         NOT NULL DEFAULT 0,
    selectors         JSONB           NOT NULL DEFAULT '{}',
    steps             JSONB           NOT NULL DEFAULT '[]',
    conditional_steps JSONB           NOT NULL DEFAULT '[]',
    enabled           BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at        TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_policies_enabled_priority ON policies (enabled, priority DESC) WHERE enabled = TRUE;
CREATE INDEX idx_policies_type ON policies (type) WHERE enabled = TRUE;
CREATE INDEX idx_policies_selectors_gin ON policies USING GIN (selectors jsonb_path_ops);

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_policies_updated_at
    BEFORE UPDATE ON policies
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
