package main

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	HetznerToken     string
	TelegramToken    string
	AllowedGroupID   int64
	AdminTGID        int64
	ServerLocation   string
	PorkbunAPIKey    string
	PorkbunSecretKey string
	PorkbunDomain    string
}

func loadConfig() (*Config, error) {
	cfg := &Config{}

	cfg.HetznerToken = os.Getenv("HETZNER_TOKEN")
	if cfg.HetznerToken == "" {
		return nil, fmt.Errorf("HETZNER_TOKEN environment variable is required")
	}

	cfg.TelegramToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	if cfg.TelegramToken == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN environment variable is required")
	}

	groupID := os.Getenv("ALLOWED_GROUP_ID")
	if groupID == "" {
		return nil, fmt.Errorf("ALLOWED_GROUP_ID environment variable is required")
	}
	var err error
	cfg.AllowedGroupID, err = strconv.ParseInt(groupID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid ALLOWED_GROUP_ID: %v", err)
	}

	adminID := os.Getenv("ADMIN_TG_ID")
	if adminID == "" {
		return nil, fmt.Errorf("ADMIN_TG_ID environment variable is required")
	}
	cfg.AdminTGID, err = strconv.ParseInt(adminID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid ADMIN_TG_ID: %v", err)
	}

	cfg.ServerLocation = os.Getenv("HETZNER_LOCATION")
	if cfg.ServerLocation == "" {
		cfg.ServerLocation = "nbg1"
	}

	cfg.PorkbunAPIKey = os.Getenv("PORKBUN_API_KEY")
	if cfg.PorkbunAPIKey == "" {
		return nil, fmt.Errorf("PORKBUN_API_KEY environment variable is required")
	}

	cfg.PorkbunSecretKey = os.Getenv("PORKBUN_SECRET_KEY")
	if cfg.PorkbunSecretKey == "" {
		return nil, fmt.Errorf("PORKBUN_SECRET_KEY environment variable is required")
	}

	cfg.PorkbunDomain = os.Getenv("PORKBUN_DOMAIN")
	if cfg.PorkbunDomain == "" {
		return nil, fmt.Errorf("PORKBUN_DOMAIN environment variable is required")
	}

	return cfg, nil
}
