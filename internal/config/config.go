package config

import "github.com/kelseyhightower/envconfig"

type Config struct {
	Token    string `envconfig:"TINVEST_TOKEN"    required:"true" default:""`
	Accounts string `envconfig:"TINVEST_ACCOUNTS"`

	// RegistryFile — реестр доходности в формате inline-полей Dataview.
	RegistryFile string `envconfig:"TINVEST_REGISTRY_FILE"`
	// RegistrySchedule — когда дописывать запись: «Mon,Fri 11:00» или «11:00»
	// для ежедневного запуска. Пусто — не обновлять по расписанию (но кнопка
	// синхронизации на странице всё равно доступна, если задан RegistryFile).
	RegistrySchedule string `envconfig:"TINVEST_REGISTRY_SCHEDULE"`

	// PortfolioFile — файл Обсидиана с таблицами долей портфеля.
	PortfolioFile string `envconfig:"TINVEST_PORTFOLIO_FILE"`
	// PortfolioSchedule — время HH:MM, в которое обновлять PortfolioFile
	// первого числа каждого квартала. Пусто — не обновлять.
	PortfolioSchedule string `envconfig:"TINVEST_PORTFOLIO_SCHEDULE"`

	HTTPAddr string `envconfig:"TINVEST_HTTP_ADDR" required:"true" default:":8080"`
}

func LoadConfig() (Config, error) {
	var cfg Config
	err := envconfig.Process("", &cfg)
	return cfg, err
}
