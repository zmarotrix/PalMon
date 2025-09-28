package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ServerState struct and its methods are unchanged
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

// forceKillProcess and isProcessRunning functions are unchanged
func forceKillProcess(pid int) error {
	LogWarn.Printf("Executing forceful taskkill on PID: %d", pid)
	cmd := exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid))
	return cmd.Run()
}
func isProcessRunning(pid int) bool {
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid))
	output, err := cmd.Output()
	if err != nil { return false }
	return strings.Contains(string(output), strconv.Itoa(pid))
}

// runHealthCheck function is unchanged
func runHealthCheck(cfg *Config) bool {
	metrics, metricsErr := GetAPIMetrics(cfg.RestAPI.Port, cfg.RCON.Password)
	players, playersErr := GetAPIPlayers(cfg.RestAPI.Port, cfg.RCON.Password)
	settings, settingsErr := GetAPISettings(cfg.RestAPI.Port, cfg.RCON.Password)
	if metricsErr != nil || playersErr != nil || settingsErr != nil {
		LogWarn.Printf("Health check failed. Errors: %v, %v, %v", metricsErr, playersErr, settingsErr)
		state.SetStatus("Unhealthy")
		return false
	}
	LogSuccess.Printf("Health check successful. FPS: %d, Players: %d/%d", metrics.ServerFPS, metrics.PlayerCount, metrics.MaxPlayers)
	state.UpdateFullState("Healthy", settings.ServerName, players.Players, metrics.MaxPlayers, metrics.ServerFPS, metrics.Uptime)
	return true
}


func runMonitor(cfg *Config) {
	for {
		LogInfo.Println("--- STARTING SERVER ---")
		state.UpdateFullState("Starting", "N/A", nil, 0, 0, 0)

		cmd := exec.Command(filepath.Join(cfg.Server.Path, cfg.Server.Executable)); cmd.Dir = cfg.Server.Path
		if err := cmd.Start(); err != nil { LogError.Fatalf("Failed to start server: %v", err) }
		serverProcess = cmd.Process
		
		pid := serverProcess.Pid
		LogInfo.Printf("Server process started with PID: %d", pid)
		sendDiscordNotification(cfg.Discord.WebhookURL, "Server process started.")
		
		processDone := make(chan error, 1); go func() { processDone <- cmd.Wait() }()

		// --- NEW, CENTRALIZED MONITORING LOOP ---
		wasResponsive := true
		startupTimer := time.NewTimer(time.Duration(cfg.Monitor.StartupDelaySec) * time.Second)
		
		// *** THIS IS THE FIX ***
		// We declare a nil channel. A select case on a nil channel is ignored, preventing the panic.
		var healthTicker *time.Ticker
		var healthTickerChan <-chan time.Time

	monitorLoop:
		for {
			select {
			case <-startupTimer.C:
				LogInfo.Println("Initial startup delay complete. Starting scheduled health checks.")
				// The ticker is created and its channel is assigned here. Now the case below will be active.
				healthTicker = time.NewTicker(time.Duration(cfg.Monitor.CheckIntervalSec) * time.Second)
				healthTickerChan = healthTicker.C

			case <-healthTickerChan:
				if !runHealthCheck(cfg) {
					LogWarn.Println("Entering rapid triage state...")
					state.SetStatus("Confirming")
					isRecovered := false
					for i := 1; i < cfg.Monitor.UnhealthyThreshold; i++ {
						time.Sleep(time.Duration(cfg.Monitor.RapidCheckIntervalSec) * time.Second)
						LogInfo.Printf("Performing rapid check %d/%d...", i+1, cfg.Monitor.UnhealthyThreshold)
						if runHealthCheck(cfg) {
							LogSuccess.Println("Server recovered during triage.")
							isRecovered = true; break
						}
					}
					if !isRecovered {
						LogError.Println("Server failed all triage checks and is confirmed unresponsive.")
						wasResponsive = false; break monitorLoop
					}
				}

			case respChan := <-ForceCheckChan:
				LogInfo.Println("Manual health check triggered by admin.")
				runHealthCheck(cfg)
				if healthTicker != nil { healthTicker.Reset(time.Duration(cfg.Monitor.CheckIntervalSec) * time.Second) }
				respChan <- true

			case err := <-processDone:
				LogWarn.Printf("Server process exited: %v", err)
				wasResponsive = true; break monitorLoop

			case action := <-AdminActionChan:
				LogInfo.Printf("Received admin action: %s", action)
				if action == "shutdown" || action == "restart" {
					if err := PostShutdown(cfg.RestAPI.Port, cfg.RCON.Password); err != nil {
						LogWarn.Printf("Graceful shutdown failed: %v.", err)
					}
				}
				wasResponsive = true; break monitorLoop
			}
		}

		// --- TERMINATION AND RESTART LOGIC ---
		if healthTicker != nil { healthTicker.Stop() }

		LogWarn.Println("--- STOPPING SERVER ---")
		state.SetStatus("Stopping")
		
		if isProcessRunning(pid) {
			LogError.Printf("Server process %d is still running. Forcing termination.", pid)
			forceKillProcess(pid)
			LogInfo.Printf("Waiting for process %d to terminate...", pid)
			for i := 0; i < 15; i++ {
				if !isProcessRunning(pid) { LogSuccess.Printf("Process %d terminated successfully.", pid); break }
				time.Sleep(1 * time.Second)
			}
		} else {
			LogInfo.Printf("Process %d already exited.", pid)
		}

		if !wasResponsive { sendDiscordNotification(cfg.Discord.WebhookURL, "Server became unresponsive and is being restarted.")
		} else { sendDiscordNotification(cfg.Discord.WebhookURL, "Server process exited and is being restarted.") }
		
		state.SetStatus("Restarting"); LogWarn.Println("Restarting in 10 seconds..."); time.Sleep(10 * time.Second)
	}
}