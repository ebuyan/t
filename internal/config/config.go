package config

import "github.com/kelseyhightower/envconfig"

type Config struct {
	HTTPAddr string `envconfig:"TINVEST_HTTP_ADDR" required:"true" default:":8080"`

	Token string `envconfig:"TINVEST_TOKEN" required:"true" default:""`

	RegistryFile     string `envconfig:"TINVEST_REGISTRY_FILE"`
	RegistrySchedule string `envconfig:"TINVEST_REGISTRY_SCHEDULE"`

	PortfolioFile     string `envconfig:"TINVEST_PORTFOLIO_FILE"`
	PortfolioSchedule string `envconfig:"TINVEST_PORTFOLIO_SCHEDULE"`
}

func LoadConfig() (Config, error) {
	var cfg Config
	err := envconfig.Process("", &cfg)
	return cfg, err
}
