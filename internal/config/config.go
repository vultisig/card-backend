package config

import (
	"errors"
	"log"

	"github.com/spf13/viper"
)

type Config struct {
	Port string
}

func Load() Config {
	viper.SetDefault("port", "8080")
	viper.AutomaticEnv()

	viper.SetConfigName("config")
	viper.SetConfigType("json")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := errors.AsType[viper.ConfigFileNotFoundError](err); !ok {
			log.Fatalf("config: %v", err)
		}
	}

	return Config{Port: viper.GetString("port")}
}
