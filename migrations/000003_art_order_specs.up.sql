CREATE TABLE art_order_specs (
    order_id UUID PRIMARY KEY,
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    work_id UUID REFERENCES art_works(id) ON DELETE SET NULL,
    work_title VARCHAR(160) NOT NULL DEFAULT '',
    options JSONB NOT NULL DEFAULT '[]',
    quantity INT NOT NULL DEFAULT 1 CHECK (quantity > 0),
    estimated_price_cents BIGINT NOT NULL DEFAULT 0,
    currency VARCHAR(5) NOT NULL DEFAULT 'CUP',
    delivery BOOLEAN NOT NULL DEFAULT FALSE,
    delivery_address TEXT NOT NULL DEFAULT '',
    gift_message TEXT NOT NULL DEFAULT '',
    needed_by TIMESTAMPTZ,
    materials_consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_art_order_specs_business ON art_order_specs(business_id, needed_by);
