package main

import (
	"bufio"
	"fmt"
	"os"
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

	// Use a channel to listen for fatal errors from concurrent services
	errChan := make(chan error, 2)

	go startWebServer(cfg, errChan)
	go runMonitor(cfg, errChan)

	// Block until an error is received from either the web server or the monitor.
	// This should never happen in normal operation.
	err = <-errChan
	return err
}