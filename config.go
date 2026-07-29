package main

import (
	"encoding/json"
	"os"
)

type Config struct {
	Server struct {
		Path               string `json:"path"`
		Executable         string `json:"executable"`
		RealExecutableName string `json:"real_executable_name"`
	} `json:"server"`
	Monitor struct {
		StartupDelaySec       int `json:"startup_delay_seconds"`
		CheckIntervalSec      int `json:"health_check_interval_seconds"`
		RapidCheckIntervalSec int `json:"rapid_check_interval_seconds"`
		UnhealthyThreshold    int `json:"unhealthy_threshold"`
	} `json:"monitor"`
	RestAPI struct {
		Enabled       bool   `json:"-"`
		Port          int    `json:"-"`
		AdminPassword string `json:"-"`
	} `json:"rest_api"`
	Web struct {
		ListenAddress string `json:"listen_address"`
		PageTitle     string `json:"page_title"`
		Username      string `json:"username"`
		Password      string `json:"password"`
	} `json:"web"`
	Discord struct {
		WebhookURL string `json:"webhook_url"`
	} `json:"discord"`
}

func loadConfig(path string) (*Config, error) {
	configFile, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config Config
	if err := json.Unmarshal(configFile, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func saveConfig(path string, config *Config) error {
	file, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, file, 0644)
}