# Design

- Price = (base + extra prices of chosen options) × quantity; required options must be chosen; unknown options fail.
- Cost = Σ material unit cost × quantity × units + labor hours × hourly rate × units; suggested price rounds up to a
  whole peso after the margin.
- `materials_consumed_at` guards against deducting stock twice.
