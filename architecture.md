# ArtCraft architecture

```
client ─► Gateway ─► ArtCraft (gRPC :50075)
                      ├─ BusinessKit: core + retail (catalog, materials stock) + orders (encargos workflow)
                      ├─ ArtCraftService: works, options, bill of materials, cost estimates, custom orders, production board
                      └─ consumer (RabbitMQ: gallery, subscription, review)
         Postgres cubalinked_artcraft · Redis db 12 · Auth · AssetsS3 · Notifications · Queue
```
