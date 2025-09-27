package main

import (
	"encoding/json"
	"os"
)

type Config struct {
	Server  struct { Path string `json:"path"`; Executable string `json:"executable"` } `json:"server"`
	Monitor struct { 
		StartupDelaySec int `json:"startup_delay_seconds"`
		CheckIntervalSec int `json:"health_check_interval_seconds"`
		// NEW: The interval for checks when the server is suspected to be down.
		RapidCheckIntervalSec int `json:"rapid_check_interval_seconds"`
		UnhealthyThreshold int `json:"unhealthy_threshold"` 
	} `json:"monitor"`
	RCON    struct { Address string `json:"address"`; Password string `json:"password"` } `json:"rcon"`
	RestAPI struct { Enabled bool `json:"enabled"`; Port int `json:"port"` } `json:"rest_api"`
	Web     struct { ListenAddress string `json:"listen_address"`; PageTitle string `json:"page_title"`; Username string `json:"username"`; Password string `json:"password"` } `json:"web"`
	Discord struct { WebhookURL string `json:"webhook_url"` } `json:"discord"`
}

func loadConfig(path string) (*Config, error) {
	configFile, err := os.ReadFile(path)
	if err != nil { return nil, err }
	var config Config
	if err := json.Unmarshal(configFile, &config); err != nil { return nil, err }
	return &config, nil
}