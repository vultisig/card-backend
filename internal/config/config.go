package config

import (
	"errors"
	"log"

	"github.com/spf13/viper"
)

type Config struct {
	Port        string
	DatabaseURL string
	JWTSecret   string
	ReapAPIKey  string
	// ReapEnv selects the REAP API environment: "sandbox" or "prod".
	ReapEnv string
}

func Load() Config {
	viper.SetDefault("port", "8080")
	viper.SetDefault("database_url", "postgres://postgres:postgres@localhost:5432/card_backend?sslmode=disable")
	viper.SetDefault("reap_env", "sandbox")
	viper.AutomaticEnv()

	viper.SetConfigName("config")
	viper.SetConfigType("json")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := errors.AsType[viper.ConfigFileNotFoundError](err); !ok {
			log.Fatalf("config: %v", err)
		}
	}

	return Config{
		Port:        viper.GetString("port"),
		DatabaseURL: viper.GetString("database_url"),
		JWTSecret:   viper.GetString("jwt_secret"),
		ReapAPIKey:  viper.GetString("reap_api_key"),
		ReapEnv:     viper.GetString("reap_env"),
	}
}
