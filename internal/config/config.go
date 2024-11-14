package config

import (
	"fmt"
	"github.com/BurntSushi/toml"
	"os"
)

type TikaConfig struct {
	Enable bool   `toml:"enable"`
	Host   string `toml:"host"`
	Port   int    `toml:"port"`
}

type GoogleConfig struct {
	Enable                 bool   `toml:"enable"`
	Url                    string `toml:"url"`
	MillisecondsPerRequest uint   `toml:"requests_per_second"`
}

type advanced struct {
	MaxCharactersToSearchForIsbn uint `toml:"max_characters_to_search_for_isbn"`
}

type Config struct {
	Tika     TikaConfig   `toml:"tika"`
	Google   GoogleConfig `toml:"google"`
	Advanced advanced     `toml:"advanced"`
}

var Defaults = map[string]any{
	"tika.port": 9998,

	"google.url":                      "www.googleapis.com/books/v1/volumes",
	"google.milliseconds_per_request": 1500,

	"advanced.max_characters_to_search_for_isbn": 10000,
}

func NewConfig(configPath string) (*Config, error) {
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config Config
	_, err = toml.Decode(string(configData), &config)
	if err != nil {
		return nil, err
	}

	err = config.Validate()
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func (c *Config) Validate() error {
	if c.Tika.Enable {
		errorMsg := "%s must be configured if tika is enabled"

		if len(c.Tika.Host) == 0 {
			return fmt.Errorf(errorMsg, "tika.host")
		}
		if c.Tika.Port == 0 {
			c.Tika.Port = Defaults["tika.port"].(int)
		}
	}

	if c.Google.Enable {
		if len(c.Google.Url) == 0 {
			c.Google.Url = Defaults["google.url"].(string)
		}
		if c.Google.MillisecondsPerRequest == 0 {
			c.Google.MillisecondsPerRequest = uint(Defaults["google.milliseconds_per_request"].(int))
		}
	}

	if c.Advanced.MaxCharactersToSearchForIsbn == 0 {
		c.Advanced.MaxCharactersToSearchForIsbn = uint(Defaults["advanced.max_characters_to_search_for_isbn"].(int))
	}

	return nil
}
