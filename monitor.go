package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ServerState struct and its methods
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

// Global variables
var (
	state         = ServerState{}
	AdminActionChan = make(chan string, 1)
	ForceCheckChan = make(chan chan bool)
)

// State machine constants
const (
	StateMonitoring = "MONITORING"
	StateStopped    = "STOPPED"
)

// Main monitor loop acting as a state machine
func runMonitor(cfg *Config, errChan chan error) {
	currentState := StateMonitoring

	for {
		switch currentState {
		case StateMonitoring:
			LogInfo.Println("--- ENTERING MONITORING STATE ---")
			state.UpdateFullState("Starting", "N/A", nil, 0, 0, 0)

			cmd := exec.Command(filepath.Join(cfg.Server.Path, cfg.Server.Executable)); cmd.Dir = cfg.Server.Path
			if err := cmd.Start(); err != nil { errChan <- fmt.Errorf("failed to start server: %w", err); return }
			
			LogInfo.Printf("Launcher process %s started with PID: %d", cfg.Server.Executable, cmd.Process.Pid)
			sendDiscordNotification(cfg.Discord.WebhookURL, "Server process started.")
			
			processDone := make(chan error, 1); go func() { processDone <- cmd.Wait() }()

			exitAction, wasResponsive := monitorLoop(cfg, processDone)
			
			LogWarn.Println("--- STOPPING SERVER PROCESS ---")
			ensureProcessStopped(cfg)
			select { case <-processDone: LogInfo.Println("Launcher handle released.") 
			case <-time.After(5 * time.Second): LogWarn.Println("Timeout waiting for launcher handle.") }

			if exitAction == "shutdown" {
				LogInfo.Println("Server shutdown requested by admin. Entering STOPPED state.")
				state.UpdateFullState("Stopped", state.ServerName, nil, 0, 0, 0)
				sendDiscordNotification(cfg.Discord.WebhookURL, "Server has been stopped by an admin.")
				currentState = StateStopped
			} else {
				if !wasResponsive { sendDiscordNotification(cfg.Discord.WebhookURL, "Server became unresponsive and is being restarted.")
				} else { sendDiscordNotification(cfg.Discord.WebhookURL, "Server process exited and is being restarted.") }
				state.SetStatus("Restarting"); LogWarn.Println("Restarting in 10 seconds..."); time.Sleep(10 * time.Second)
			}

		case StateStopped:
			LogInfo.Println("Server is in STOPPED state. Waiting for admin action...")
			action := <-AdminActionChan
			if action == "start" {
				LogInfo.Println("Start command received from admin. Transitioning to MONITORING state.")
				currentState = StateMonitoring
			}
		}
	}
}

// Core monitoring logic for when the server is running
func monitorLoop(cfg *Config, processDone chan error) (exitAction string, wasResponsive bool) {
	LogInfo.Printf("Waiting %d seconds for server to initialize...", cfg.Monitor.StartupDelaySec)
	state.SetStatus(fmt.Sprintf("Initializing (%ds)", cfg.Monitor.StartupDelaySec))
	
	startupTimer := time.NewTimer(time.Duration(cfg.Monitor.StartupDelaySec) * time.Second)
	var healthTicker *time.Ticker
	var healthTickerChan <-chan time.Time
	
	for {
		select {
		case <-startupTimer.C:
			LogInfo.Println("Initial startup delay complete. Starting scheduled health checks.")
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
					if runHealthCheck(cfg) { LogSuccess.Println("Server recovered during triage."); isRecovered = true; break }
				}
				if !isRecovered {
					LogError.Println("Server failed all triage checks and is confirmed unresponsive.")
					if healthTicker != nil { healthTicker.Stop() }; return "crash", false
				}
			}

		case respChan := <-ForceCheckChan:
			LogInfo.Println("Manual health check triggered by admin."); runHealthCheck(cfg)
			if healthTicker != nil { healthTicker.Reset(time.Duration(cfg.Monitor.CheckIntervalSec) * time.Second) }
			respChan <- true

		case err := <-processDone:
			LogWarn.Printf("Launcher process exited: %v", err)
			if healthTicker != nil { healthTicker.Stop() }; return "exit", true

		case action := <-AdminActionChan:
			LogInfo.Printf("Received admin action: %s", action)
			if action == "shutdown" || action == "restart" {
				if err := PostShutdown(cfg.RestAPI.Port, cfg.RCON.Password); err != nil {
					LogWarn.Printf("Graceful shutdown failed: %v.", err)
				}
			}
			if healthTicker != nil { healthTicker.Stop() }; return action, true
		}
	}
}

// --- Helper Functions ---
func ensureProcessStopped(cfg *Config) {
	realServerExe := cfg.Server.RealExecutableName
	if isProcessRunningByName(realServerExe) {
		forceKillProcessByName(realServerExe)
		LogInfo.Printf("Waiting for process %s to terminate...", realServerExe)
		for i := 0; i < 15; i++ {
			if !isProcessRunningByName(realServerExe) { LogSuccess.Printf("Process %s terminated.", realServerExe); return }
			time.Sleep(1 * time.Second)
		}
		LogError.Printf("Process %s failed to terminate.", realServerExe)
	} else {
		LogInfo.Printf("Process %s already exited.", realServerExe)
	}
}
func forceKillProcessByName(executableName string) error {
	LogWarn.Printf("Executing forceful taskkill on executable: %s", executableName)
	cmd := exec.Command("taskkill", "/F", "/IM", executableName, "/T")
	return cmd.Run()
}
func isProcessRunningByName(executableName string) bool {
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("IMAGENAME eq %s", executableName))
	output, err := cmd.Output()
	if err != nil { return false }
	return strings.Contains(string(output), executableName)
}
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