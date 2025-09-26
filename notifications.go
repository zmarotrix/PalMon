package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

func sendDiscordNotification(webhookURL, message string) {
	if webhookURL == "" {
		return
	}

	log.Printf("Sending Discord notification: %s", message)

	payload := map[string]string{
		"content": fmt.Sprintf(":robot: **PalMon:** %s", message),
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error creating Discord payload: %v", err)
		return
	}

	req, err := http.NewRequest("POST", webhookURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		log.Printf("Error creating Discord request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error sending Discord notification: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("Discord returned non-success status code: %d", resp.StatusCode)
	}
}