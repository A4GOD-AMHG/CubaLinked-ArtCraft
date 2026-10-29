CREATE TABLE art_work_materials (
    work_id UUID NOT NULL REFERENCES art_works(id) ON DELETE CASCADE,
    product_id UUID NOT NULL,
    quantity NUMERIC(12,3) NOT NULL CHECK (quantity > 0),
    PRIMARY KEY (work_id, product_id)
);
