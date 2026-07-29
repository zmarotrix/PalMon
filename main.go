package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Printf("\n\n--- A FATAL ERROR OCCURRED ---\n")
		fmt.Printf("ERROR: %v\n", err)
		fmt.Printf("Press Enter to exit...")
		bufio.NewReader(os.Stdin).ReadBytes('\n')
	}
}

func run() error {
	initLogging()

	LogInfo.Println("Loading configuration from config.json...")
	cfg, err := loadConfig("config.json")
	if err != nil {
		return fmt.Errorf("failed to load config.json: %w", err)
	}

	// --- AUTO-CONFIGURATION AND PATH VALIDATION ---
	LogInfo.Println("Verifying Palworld server path and settings...")

	var port int
	var password string

	for { // Loop until we get a valid path and settings
		port, password, err = EnsureSettings(cfg.Server.Path)
		if err == nil {
			// Success! Overwrite config in memory with the real values.
			cfg.RestAPI.Port = port
			cfg.RestAPI.AdminPassword = password
			LogSuccess.Printf("Palworld settings verified! Dynamically bound to Port: %d", port)
			break // Exit the setup loop
		}

		// If we're here, EnsureSettings failed, likely due to a bad path.
		fmt.Printf("\n--- PALMON SETUP ---\n")
		LogError.Printf("Could not find server files at the provided path: %s", cfg.Server.Path)
		fmt.Printf("Please enter the full path to your PalServer directory (e.g., C:\\Steam\\steamapps\\common\\PalServer):\n> ")
		
		// Read new path from user
		reader := bufio.NewReader(os.Stdin)
		newPath, _ := reader.ReadString('\n')
		newPath = strings.TrimSpace(newPath)

		if newPath != "" {
			cfg.Server.Path = newPath
			// Attempt to save the new path so the user only does this once.
			if err := saveConfig("config.json", cfg); err != nil {
				LogWarn.Printf("Warning: Could not save new path to config.json: %v", err)
			} else {
				LogSuccess.Printf("New server path saved to config.json!")
			}
		}
	}
	// --- END AUTO-CONFIGURATION ---

	errChan := make(chan error, 2)
	go startWebServer(cfg, errChan)
	go runMonitor(cfg, errChan)

	err = <-errChan
	return err
}