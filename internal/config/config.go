// Package config loads the vertical's configuration: the BusinessKit base settings plus RabbitMQ.
package config

import kitcfg "github.com/A4GOD-AMHG/CubaLinked-BusinessKitModule/pkg/config"

// EnvPrefix is the prefix of the environment variables that override config.yaml.
const EnvPrefix = "ARTCRAFT"

// Config is the full configuration of the vertical service.
type Config struct {
	kitcfg.Base `mapstructure:",squash"`
	RabbitMQ    RabbitMQConfig `mapstructure:"rabbitmq"`
}

// RabbitMQConfig points the event consumer at the broker and the CloudEvents exchange.
type RabbitMQConfig struct {
	URL      string `mapstructure:"url"`
	Exchange string `mapstructure:"exchange"`
	Prefetch int    `mapstructure:"prefetch"`
}

// Load reads config.yaml and the environment, applies the kit defaults and fills the vertical id,
// exchange and prefetch when they are missing.
func Load(path string) (*Config, error) {
	var cfg Config
	if err := kitcfg.Load(EnvPrefix, path, &cfg); err != nil {
		return nil, err
	}
	cfg.Base.ApplyDefaults()
	if cfg.Platform.Vertical == "" {
		cfg.Platform.Vertical = "artcraft"
	}
	if cfg.RabbitMQ.Exchange == "" {
		cfg.RabbitMQ.Exchange = "cubalinked.events"
	}
	if cfg.RabbitMQ.Prefetch <= 0 {
		cfg.RabbitMQ.Prefetch = 10
	}
	return &cfg, nil
}
