package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// APIMetrics now correctly includes all fields from the /metrics endpoint
type APIMetrics struct {
	PlayerCount    int     `json:"currentplayernum"`
	ServerFPS      int     `json:"serverfps"`
	MaxPlayers     int     `json:"maxplayernum"`
	Uptime         uint64  `json:"uptime"`
}

type APIPlayer struct {
	Name   string `json:"name"`
	Level  int    `json:"level"`
	UserID string `json:"userId"`
}

type APIPlayers struct {
	Players []APIPlayer `json:"players"`
}

type APISettings struct {
	ServerName string `json:"ServerName"`
}

// Helper function to perform an authenticated GET request
func makeAPIRequest(url string, password string, target interface{}) error {
	req, err := http.NewRequest("GET", url, nil); if err != nil { return fmt.Errorf("could not create request: %w", err) }
	if password != "" { req.SetBasicAuth("admin", password) }
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req); if err != nil { return fmt.Errorf("request failed: %w", err) }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK { return fmt.Errorf("received non-200 status code: %d", resp.StatusCode) }
	return json.NewDecoder(resp.Body).Decode(target)
}

func PostAPIRequest(url string, password string, body interface{}) error {
	jsonBody, err := json.Marshal(body); if err != nil { return fmt.Errorf("could not marshal json body: %w", err) }
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody)); if err != nil { return fmt.Errorf("could not create post request: %w", err) }
	req.Header.Set("Content-Type", "application/json")
	if password != "" { req.SetBasicAuth("admin", password) }
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req); if err != nil { return fmt.Errorf("post request failed: %w", err) }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK { return fmt.Errorf("post request received non-200 status code: %d", resp.StatusCode) }
	return nil
}

func GetAPIMetrics(port int, password string) (*APIMetrics, error) {
	var metrics APIMetrics
	return &metrics, makeAPIRequest(fmt.Sprintf("http://127.0.0.1:%d/v1/api/metrics", port), password, &metrics)
}
func GetAPIPlayers(port int, password string) (*APIPlayers, error) {
	var players APIPlayers
	return &players, makeAPIRequest(fmt.Sprintf("http://127.0.0.1:%d/v1/api/players", port), password, &players)
}
func GetAPISettings(port int, password string) (*APISettings, error) {
	var settings APISettings
	return &settings, makeAPIRequest(fmt.Sprintf("http://127.0.0.1:%d/v1/api/settings", port), password, &settings)
}
func PostKickPlayer(port int, password string, userID string) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/api/kick", port)
	body := map[string]string{"userid": userID, "message": "Kicked by admin."}
	return PostAPIRequest(url, password, body)
}
func PostBanPlayer(port int, password string, userID string) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/api/ban", port)
	body := map[string]string{"userid": userID, "message": "Banned by admin."}
	return PostAPIRequest(url, password, body)
}
func PostBroadcast(port int, password string, message string) error {
	// *** THIS IS THE FIX ***
	// The endpoint is /announce, not /broadcast.
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/api/announce", port)
	body := map[string]string{"message": message}
	return PostAPIRequest(url, password, body)
}
func PostShutdown(port int, password string) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/api/shutdown", port)
	body := map[string]string{"waittime": "5", "message": "Server is shutting down now."}
	return PostAPIRequest(url, password, body)
}