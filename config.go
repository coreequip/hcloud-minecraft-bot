package main

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
)

type Config struct {
	HetznerToken     string `env:"HETZNER_TOKEN" required:"true"`
	TelegramToken    string `env:"TELEGRAM_BOT_TOKEN" required:"true"`
	AllowedGroupID   int64  `env:"ALLOWED_GROUP_ID" required:"true"`
	AdminTGID        int64  `env:"ADMIN_TG_ID" required:"true"`
	ServerLocation   string `env:"HETZNER_LOCATION" default:"nbg1"`
	PorkbunAPIKey    string `env:"PORKBUN_API_KEY" required:"true"`
	PorkbunSecretKey string `env:"PORKBUN_SECRET_KEY" required:"true"`
	PorkbunDomain    string `env:"PORKBUN_DOMAIN" required:"true"`
}

func loadConfig() (*Config, error) {
	cfg := &Config{}

	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)

		envName := field.Tag.Get("env")
		if envName == "" {
			continue
		}

		envValue := os.Getenv(envName)
		required := field.Tag.Get("required") == "true"
		defaultValue := field.Tag.Get("default")

		// Handle empty values
		if envValue == "" {
			if required {
				return nil, fmt.Errorf("%s environment variable is required", envName)
			}
			if defaultValue != "" {
				envValue = defaultValue
			}
		}

		// Set the value based on field type
		switch fieldValue.Kind() {
		case reflect.String:
			fieldValue.SetString(envValue)
		case reflect.Int64:
			if envValue != "" {
				intValue, err := strconv.ParseInt(envValue, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid %s: %v", envName, err)
				}
				fieldValue.SetInt(intValue)
			}
		case reflect.Bool:
			if envValue != "" {
				boolValue, err := strconv.ParseBool(envValue)
				if err != nil {
					return nil, fmt.Errorf("invalid %s: %v (expected true/false, 1/0, etc.)", envName, err)
				}
				fieldValue.SetBool(boolValue)
			}
		default:
			return nil, fmt.Errorf("unsupported field type for %s", field.Name)
		}
	}

	return cfg, nil
}
