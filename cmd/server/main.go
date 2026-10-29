// Command server runs the ArtCraft vertical for handmade works and custom orders (encargos):
// BusinessKit core, retail and orders plus the artcraft.v1 works, materials and production board.
package main

import (
	"context"
	"os"

	"github.com/rs/zerolog/log"

	"github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/pkg/orders"
	"github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/pkg/retail"
	"github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/pkg/server"

	"github.com/A4GOD-AMHG/CubaLinked-ArtCraftModule/internal/config"
	"github.com/A4GOD-AMHG/CubaLinked-ArtCraftModule/internal/consumer"
	"github.com/A4GOD-AMHG/CubaLinked-ArtCraftModule/internal/service"
	"github.com/A4GOD-AMHG/CubaLinked-ArtCraftModule/migrations"
	artcraftv1 "github.com/A4GOD-AMHG/CubaLinked-ArtCraftModule/proto/artcraft/v1"
)

func main() {
	path := os.Getenv("ARTCRAFT_CONFIG")
	if path == "" {
		path = "configs/config.yaml"
	}
	cfg, err := config.Load(path)
	if err != nil {
		log.Fatal().Err(err).Msg("load config")
	}
	err = server.Run(&cfg.Base, server.Options{
		Capabilities:       []server.Capability{retail.Capability(retail.Hooks{}), orders.Capability(orders.Hooks{})},
		VerticalMigrations: migrations.FS,
		ExtraPublicMethods: service.PublicMethods(),
		Register: func(ctx context.Context, app *server.App) error {
			artcraftv1.RegisterArtCraftServiceServer(app.GRPC, service.New(app))
			consumer.New(app, cfg.RabbitMQ.URL, cfg.RabbitMQ.Exchange, cfg.RabbitMQ.Prefetch).Start(ctx)
			return nil
		},
	})
	if err != nil {
		log.Fatal().Err(err).Msg("server stopped with error")
	}
}
