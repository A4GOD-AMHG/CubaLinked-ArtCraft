CREATE TABLE art_works (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    title VARCHAR(160) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    image_file_ids TEXT[] NOT NULL DEFAULT '{}',
    category VARCHAR(30) NOT NULL DEFAULT 'other_craft',
    techniques TEXT[] NOT NULL DEFAULT '{}',
    occasions TEXT[] NOT NULL DEFAULT '{}',
    made_to_order BOOLEAN NOT NULL DEFAULT TRUE,
    base_price_cents BIGINT NOT NULL DEFAULT 0 CHECK (base_price_cents >= 0),
    currency VARCHAR(5) NOT NULL DEFAULT 'CUP',
    production_days INT NOT NULL DEFAULT 3 CHECK (production_days >= 0),
    labor_hours NUMERIC(8,2) NOT NULL DEFAULT 0,
    options JSONB NOT NULL DEFAULT '[]',
    retail_product_id UUID,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX idx_art_works_business ON art_works(business_id, sort_order) WHERE deleted_at IS NULL;
CREATE INDEX idx_art_works_occasions ON art_works USING GIN (occasions);
