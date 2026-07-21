package config

import "github.com/kelseyhightower/envconfig"

type Config struct {
	Token    string `envconfig:"TINVEST_TOKEN"    required:"true" default:"t.zUGspUsyoO1VxXgwGjx0EDMZyGAsbdPyqbGNhrdUlxNpva85athi7Pbh4L-XNbWHl4gJQuT7w7C5ljz5Nb_04w"`
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

	// Backup — класть копию файла рядом перед каждой записью. Обсидиан такие
	// файлы не показывает (расширение не .md), но они синкаются и копятся,
	// поэтому после обкатки достаточно выставить false.
	Backup bool `envconfig:"TINVEST_BACKUP" default:"true"`
}

func LoadConfig() (Config, error) {
	var cfg Config
	err := envconfig.Process("", &cfg)
	return cfg, err
}
