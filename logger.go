package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/mattn/go-colorable"
)

const (
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorReset  = "\033[0m"
)

var (
	LogInfo  *log.Logger
	LogWarn  *log.Logger
	LogSuccess *log.Logger
	LogError *log.Logger
)

func initLogging() {
	logFile, err := os.OpenFile("palmon_log.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}

	// Use colorable for Windows console compatibility
	colorableStdout := colorable.NewColorable(os.Stdout)

	// Log to both file and colorized console
	multiWriter := io.MultiWriter(logFile, colorableStdout)

	LogInfo = log.New(multiWriter, "", log.Ldate|log.Ltime)
	LogWarn = log.New(multiWriter, fmt.Sprintf("%s[WARN] %s", ColorYellow, ColorReset), log.Ldate|log.Ltime)
	LogSuccess = log.New(multiWriter, fmt.Sprintf("%s[SUCCESS] %s", ColorGreen, ColorReset), log.Ldate|log.Ltime)
	LogError = log.New(multiWriter, fmt.Sprintf("%s[ERROR] %s", ColorRed, ColorReset), log.Ldate|log.Ltime)
}