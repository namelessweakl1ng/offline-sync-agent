package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	AuthToken string `yaml:"auth_token"`
	ServerURL string `yaml:"server_url"`
}

var AppConfig Config

func LoadConfig() error {
	data, err := os.ReadFile("configs/config.yaml")
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(data, &AppConfig)
	if err != nil {
		return err
	}

	return nil
}
