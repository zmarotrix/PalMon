package main

import (
	"io"
	"log"
	"os"
)

func main() {
	logFile, err := os.OpenFile("palmon_log.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil { log.Fatalf("Failed to open log file: %v", err) }
	multiWriter := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(multiWriter)

	log.Println("Loading configuration from config.json...")
	cfg, err := loadConfig("config.json")
	if err != nil { log.Fatalf("Failed to load config.json: %v", err) }

	go startWebServer(cfg)
	runMonitor(cfg)
}