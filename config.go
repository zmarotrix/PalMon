package main

import (
	"encoding/json"
	"os"
)

type Config struct {
	Server struct {
		Path              string `json:"path"`
		Executable        string `json:"executable"`
		RealExecutableName string `json:"real_executable_name"`
	} `json:"server"`
	Monitor struct {
		StartupDelaySec       int `json:"startup_delay_seconds"`
		CheckIntervalSec      int `json:"health_check_interval_seconds"`
		RapidCheckIntervalSec int `json:"rapid_check_interval_seconds"`
		UnhealthyThreshold    int `json:"unhealthy_threshold"`
	} `json:"monitor"`
	RCON struct { 
		// Kept for backward compatibility with old config.json files
		Address  string `json:"address"`
		Password string `json:"password"`
	} `json:"rcon"`
	RestAPI struct {
		Enabled       bool   `json:"enabled"`
		Port          int    `json:"port"`
		AdminPassword string `json:"admin_password"` // NEW for 1.0!
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

// GetAdminPassword safely transitions to the new 1.0 setting 
// while keeping old configurations functional.
func (c *Config) GetAdminPassword() string {
	if c.RestAPI.AdminPassword != "" {
		return c.RestAPI.AdminPassword
	}
	return c.RCON.Password // Fallback to legacy setting
}