package main

func main() {
	// --- Setup Logging ---
	initLogging()

	// --- Load Configuration ---
	LogInfo.Println("Loading configuration from config.json...")
	cfg, err := loadConfig("config.json")
	if err != nil {
		LogError.Fatalf("Failed to load config.json: %v", err)
	}

	// --- Start Services ---
	go startWebServer(cfg)
	runMonitor(cfg)
}