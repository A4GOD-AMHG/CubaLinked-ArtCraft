# CubaLinked-ArtCraftModule — agent guide

- Vertical service: `ArtCraftService` (proto/artcraft/v1). Custom orders are kit `orders` (kind CUSTOM) plus `art_order_specs`.
- Capabilities installed: core + retail + orders. Materials are retail products with `is_for_sale=false`.
- Tables: art_works, art_work_materials, art_order_specs. One migration file per table.
- No comments in code. English identifiers and docs, Spanish user-facing text and public routes.
