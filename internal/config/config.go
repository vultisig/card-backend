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
	// ReapWebhookSecret is the signing secret REAP issues when a webhook
	// endpoint is registered (POST /webhooks), used to verify the
	// X-Reap-Webhook-Signature header on incoming webhook deliveries.
	ReapWebhookSecret string
	// StatsDAddr is the host:port of a DogStatsD agent (UDP). Defaults to
	// the standard local Datadog agent address; metrics are silently
	// dropped if nothing is listening there.
	StatsDAddr string
	// NotificationBaseURL is the base URL of the Vultisig notification
	// service (POST {base}/notify), used to push a client notification when
	// a REAP webhook event affects a known vault.
	NotificationBaseURL string
}

func Load() Config {
	viper.SetDefault("port", "8080")
	viper.SetDefault("database_url", "postgres://postgres:postgres@localhost:5432/card_backend?sslmode=disable")
	viper.SetDefault("reap_env", "sandbox")
	viper.SetDefault("statsd_addr", "127.0.0.1:8125")
	viper.SetDefault("notification_base_url", "https://api.vultisig.com/notification")
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
		Port:                viper.GetString("port"),
		DatabaseURL:         viper.GetString("database_url"),
		JWTSecret:           viper.GetString("jwt_secret"),
		ReapAPIKey:          viper.GetString("reap_api_key"),
		ReapEnv:             viper.GetString("reap_env"),
		ReapWebhookSecret:   viper.GetString("reap_webhook_secret"),
		StatsDAddr:          viper.GetString("statsd_addr"),
		NotificationBaseURL: viper.GetString("notification_base_url"),
	}
}
