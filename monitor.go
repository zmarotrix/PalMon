package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)


type ServerState struct {
	mu          sync.RWMutex
	Status      string; ServerName  string; Players     []APIPlayer
	MaxPlayers  int; ServerFPS   int; Uptime      uint64
}
func (s *ServerState) SetStatus(status string) { s.mu.Lock(); defer s.mu.Unlock(); s.Status = status }
func (s *ServerState) UpdateFullState(status, serverName string, players []APIPlayer, maxPlayers, fps int, uptime uint64) {
	s.mu.Lock(); defer s.mu.Unlock()
	s.Status = status; s.ServerName = serverName; s.Players = players
	s.MaxPlayers = maxPlayers; s.ServerFPS = fps; s.Uptime = uptime
}
func (s *ServerState) GetFullState() (string, string, []APIPlayer, int, int, uint64) {
	s.mu.RLock(); defer s.mu.RUnlock()
	return s.Status, s.ServerName, s.Players, s.MaxPlayers, s.ServerFPS, s.Uptime
}


var (
	serverProcess *os.Process
	state         = ServerState{}
	AdminActionChan = make(chan string, 1)
	ForceCheckChan = make(chan chan bool)
)

func runMonitor(cfg *Config) {
	for {
		log.Println("--- STARTING SERVER ---"); state.UpdateFullState("Starting", "N/A", nil, 0, 0, 0)
		cmd := exec.Command(filepath.Join(cfg.Server.Path, cfg.Server.Executable)); cmd.Dir = cfg.Server.Path
		if err := cmd.Start(); err != nil { log.Fatalf("Failed to start server: %v", err) }
		serverProcess = cmd.Process; log.Printf("Server process started with PID: %d", serverProcess.Pid)
		sendDiscordNotification(cfg.Discord.WebhookURL, "Server process started.")
		processDone := make(chan error, 1); go func() { processDone <- cmd.Wait() }()
		wasResponsive := monitorHealth(cfg, processDone)
		log.Println("--- STOPPING SERVER ---"); state.SetStatus("Stopping")
		if err := serverProcess.Kill(); err != nil { log.Printf("Failed to kill server process: %v", err) }
		<-processDone
		if !wasResponsive { sendDiscordNotification(cfg.Discord.WebhookURL, "Server became unresponsive and is being restarted.")
		} else { sendDiscordNotification(cfg.Discord.WebhookURL, "Server process exited and is being restarted.") }
		state.SetStatus("Restarting"); log.Println("Restarting in 10 seconds..."); time.Sleep(10 * time.Second)
	}
}

func runHealthCheck(cfg *Config) bool {
	log.Println("Performing REST API health check...")
	metrics, metricsErr := GetAPIMetrics(cfg.RestAPI.Port, cfg.RCON.Password)
	players, playersErr := GetAPIPlayers(cfg.RestAPI.Port, cfg.RCON.Password)
	settings, settingsErr := GetAPISettings(cfg.RestAPI.Port, cfg.RCON.Password)

	if metricsErr != nil || playersErr != nil || settingsErr != nil {
		log.Printf("Health check failed. Errors: %v, %v, %v", metricsErr, playersErr, settingsErr)
		state.SetStatus("Unhealthy")
		return false // Check failed
	}
	
	log.Printf("Health check successful. FPS: %d, Players: %d/%d", metrics.ServerFPS, metrics.PlayerCount, metrics.MaxPlayers)
	state.UpdateFullState("Healthy", settings.ServerName, players.Players, metrics.MaxPlayers, metrics.ServerFPS, metrics.Uptime)
	return true // Check succeeded
}


func monitorHealth(cfg *Config, processDone chan error) (wasResponsive bool) {
	log.Printf("Waiting %d seconds for server to initialize...", cfg.Monitor.StartupDelaySec)
	state.SetStatus(fmt.Sprintf("Initializing (%ds)", cfg.Monitor.StartupDelaySec))
	select {
	case err := <-processDone: log.Printf("Server process exited during startup: %v", err); return true
	case <-time.After(time.Duration(cfg.Monitor.StartupDelaySec) * time.Second): log.Println("Initial startup delay complete.")
	}

	healthTicker := time.NewTicker(time.Duration(cfg.Monitor.CheckIntervalSec) * time.Second); defer healthTicker.Stop()
	unhealthyCount := 0

	for {
		select {
		// Case for the automatic scheduled check
		case <-healthTicker.C:
			if !runHealthCheck(cfg) {
				unhealthyCount++
				log.Printf("Unhealthy count: %d/%d", unhealthyCount, cfg.Monitor.UnhealthyThreshold)
				if unhealthyCount >= cfg.Monitor.UnhealthyThreshold { log.Printf("Unhealthy threshold reached."); return false }
			} else {
				unhealthyCount = 0
			}
		
		// Case for the manual refresh request
		case respChan := <-ForceCheckChan:
			log.Println("Manual health check triggered by admin.")
			runHealthCheck(cfg)
			// Reset the timer to avoid an immediate follow-up check
			healthTicker.Reset(time.Duration(cfg.Monitor.CheckIntervalSec) * time.Second)
			respChan <- true // Signal to the web handler that the check is complete

		case err := <-processDone: log.Printf("Server process exited: %v", err); return true
		case action := <-AdminActionChan:
			log.Printf("Received admin action: %s", action)
			if action == "shutdown" || action == "restart" {
				if err := PostShutdown(cfg.RestAPI.Port, cfg.RCON.Password); err != nil {
					log.Printf("Graceful shutdown failed: %v. Killing process instead.", err)
				}
				return true
			}
		}
	}
}