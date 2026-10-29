# CubaLinked-ArtCraftModule

Handcraft vertical: artisans and small workshops that sell handmade works and take custom orders (encargos):
eternal sunflowers and preserved flowers, gifts, decor, jewelry, ceramics, textiles, wood, painting, party and
personalized items. Built on **BusinessKit** (core + retail + orders): catalog products, stock (materials are retail
products marked as internal supply) and the custom order workflow (quote, deposit, status, chat) come from the kit.

## Features

- Works portfolio with photos, category, techniques, occasions (Día de las Madres, San Valentín, 15 años, bodas...),
  base price, production days, labor hours and configurable options (color, size, number of flowers, free-text
  message) with extra prices.
- Bill of materials per work linked to retail supplies; cost estimate (materials + labor), suggested price with a
  margin, margin at the current price and missing-stock warnings.
- Custom orders: customers pick a work and options, quantity, reference photos, needed-by date (respecting the
  production days), pickup or delivery address and a gift message. The price estimate is stored; the order follows
  the kit flow (quote, deposit, accept, in progress, ready, delivered). Free-form encargos are allowed.
- Material consumption per order deducts stock once and moves the order to in progress.
- Production board grouped by status, sorted by needed-by date, with late and due-this-week counters.

## Run

| Setting | Value |
|---|---|
| gRPC port | 50075 |
| Env prefix | `ARTCRAFT_` |
| Database | `cubalinked_artcraft` |
| Redis DB | 12 |
| Vertical id | `artcraft` |

```bash
GOFLAGS=-mod=mod go run ./cmd/server
docker build -f CubaLinked-ArtCraftModule/Dockerfile -t cubalinked-artcraft .
```

## Events

Published: `artcraft.custom_order_requested`, `gallery.image_uploaded` (product_shot pipeline for new works), plus kit order events.

## Gateway routes (Spanish)

| Method & path | RPC |
|---|---|
| GET `/artesania/{negocio}/perfil` | GetPublicProfile |
| GET `/artesania/{negocio}/obras?ocasion=dia-de-las-madres` · GET `/artesania/{negocio}/obras/{id}` | ListWorks / GetWork |
| POST `/artesania/{negocio}/encargos` · GET `/artesania/{negocio}/encargos/{id}` | RequestCustomOrder / GetCustomOrder |
| Panel: `/panel/artesania/{negocio}/obras`, `/obras/{id}/materiales`, `/obras/{id}/costo`, `/encargos/{id}/materiales`, `/produccion` | management RPCs |
