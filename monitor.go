package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ServerState struct and methods
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

// SyncSettingsBeforeLaunch ensures the live PalWorldSettings.ini matches our desired config.
func SyncSettingsBeforeLaunch(cfg *Config) (string, error) {
	LogInfo.Println("Checking for pending setting changes before launch...")
	desiredSettings, err := LoadPalMonSettings()
	if err != nil {
		LogInfo.Println("No PalMon settings file found. Skipping sync.")
		return cfg.RestAPI.AdminPassword, nil
	}
	if err := SaveINI(cfg.Server.Path, desiredSettings); err != nil {
		return "", fmt.Errorf("failed to sync settings to PalWorldSettings.ini: %w", err)
	}
	LogSuccess.Println("Successfully synced PalMon settings to PalWorldSettings.ini!")
	syncedPassword := strings.Trim(desiredSettings["AdminPassword"], `"`)
	return syncedPassword, nil
}

// Main monitor loop acting as a state machine
func runMonitor(cfg *Config, errChan chan error) {
	currentState := StateMonitoring

	for {
		switch currentState {
		case StateMonitoring:
			LogInfo.Println("--- ENTERING MONITORING STATE ---")
			state.UpdateFullState("Starting", "N/A", nil, 0, 0, 0)
			
			syncedPassword, err := SyncSettingsBeforeLaunch(cfg)
			if err != nil {
				errChan <- err
				return
			}
			cfg.RestAPI.AdminPassword = syncedPassword
			
			cmd := exec.Command(filepath.Join(cfg.Server.Path, cfg.Server.Executable)); cmd.Dir = cfg.Server.Path
			if err := cmd.Start(); err != nil { errChan <- fmt.Errorf("failed to start server: %w", err); return }
			
			launcherPID := cmd.Process.Pid
			LogInfo.Printf("Launcher process %s started with PID: %d", cfg.Server.Executable, launcherPID)
			sendDiscordNotification(cfg.Discord.WebhookURL, "Server process started.")
			
			processDone := make(chan error, 1); go func() { processDone <- cmd.Wait() }()

			exitAction, wasResponsive := monitorLoop(cfg, processDone)
			
			LogWarn.Println("--- STOPPING SERVER PROCESS ---")
			ensureProcessTreeStopped(launcherPID)
			
			select { case <-processDone: LogInfo.Println("Launcher handle released.") 
			case <-time.After(5 * time.Second): LogWarn.Println("Timeout waiting for launcher handle.") }

			if exitAction == "shutdown" || exitAction == "config_error" {
				LogInfo.Println("Server halted. Entering STOPPED state.")
				state.UpdateFullState("Stopped", state.ServerName, nil, 0, 0, 0)
				
				if exitAction == "config_error" {
					sendDiscordNotification(cfg.Discord.WebhookURL, "CRITICAL ERROR: PalMon stopped due to an authorization issue.")
				} else {
					sendDiscordNotification(cfg.Discord.WebhookURL, "Server has been stopped by an admin.")
				}
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
// Core monitoring logic for when the server is running
func monitorLoop(cfg *Config, processDone chan error) (exitAction string, wasResponsive bool) {
	LogInfo.Printf("Waiting %d seconds for server to initialize...", cfg.Monitor.StartupDelaySec)
	state.SetStatus(fmt.Sprintf("Initializing (%ds)", cfg.Monitor.StartupDelaySec))

	// This timer controls the initial wait. After it fires, we start the main checks.
	startupTimer := time.NewTimer(time.Duration(cfg.Monitor.StartupDelaySec) * time.Second)
	<-startupTimer.C // Block until the initial startup delay has passed.

	LogInfo.Println("Initial startup delay complete. Starting health checks.")
	
	// Create the ticker for scheduled checks.
	healthTicker := time.NewTicker(time.Duration(cfg.Monitor.CheckIntervalSec) * time.Second)
	defer healthTicker.Stop() // Ensure the ticker is cleaned up when we exit.

	// --- THIS IS THE NEW, CLEANER LOGIC ---
	// We perform the first check immediately, then loop for all subsequent checks.
	for {
		isHealthy, err := runHealthCheck(cfg)
		if err != nil && strings.Contains(err.Error(), "Unauthorized") {
			LogError.Println("CRITICAL: " + err.Error())
			return "config_error", true
		}

		if !isHealthy {
			LogWarn.Println("Health check failed. Entering rapid triage state...")
			isRecovered := false
			for i := 1; i < cfg.Monitor.UnhealthyThreshold; i++ {
				time.Sleep(time.Duration(cfg.Monitor.RapidCheckIntervalSec) * time.Second)
				LogInfo.Printf("Performing rapid check %d/%d...", i+1, cfg.Monitor.UnhealthyThreshold)
				
				healthy, _ := runHealthCheck(cfg)
				if healthy {
					LogSuccess.Println("Server recovered during triage.")
					isRecovered = true
					break
				}
			}

			if !isRecovered {
				LogError.Println("Server failed all triage checks and is confirmed unresponsive.")
				return "crash", false
			}
		}

		// If we reach here, the server is healthy. Now we wait for the next event.
		select {
		case <-healthTicker.C:
			// Time for the next scheduled check, the loop will repeat.
			continue 

		case respChan := <-ForceCheckChan:
			LogInfo.Println("Manual health check triggered by admin."); runHealthCheck(cfg)
			healthTicker.Reset(time.Duration(cfg.Monitor.CheckIntervalSec) * time.Second)
			respChan <- true

		case err := <-processDone:
			LogWarn.Printf("Launcher process exited: %v", err)
			return "exit", true

		case action := <-AdminActionChan:
			LogInfo.Printf("Received admin action: %s", action)
			if action == "shutdown" || action == "restart" {
				if err := PostShutdown(cfg.RestAPI.Port, cfg.RestAPI.AdminPassword); err != nil {
					LogWarn.Printf("Graceful shutdown failed: %v.", err)
				}
			}
			return action, true
		}
	}
}

// --- Helper Functions ---
func ensureProcessTreeStopped(pid int) {
	LogWarn.Printf("Executing forceful taskkill on PID %d and its children...", pid)
	cmd := exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid), "/T")
	if err := cmd.Run(); err != nil {
		LogWarn.Printf("Taskkill command failed (process may have already ended): %v", err)
	} else {
		LogSuccess.Printf("Process tree for PID %d terminated.", pid)
	}
}

// runHealthCheck performs a check against the server's REST API endpoints.
// THIS IS THE FUNCTION THAT WAS ACCIDENTALLY DELETED. IT IS NOW RESTORED.
func runHealthCheck(cfg *Config) (bool, error) {
	pw := cfg.RestAPI.AdminPassword
	metrics, metricsErr := GetAPIMetrics(cfg.RestAPI.Port, pw)
	if metricsErr != nil && strings.Contains(metricsErr.Error(), "Unauthorized") {
		state.SetStatus("Config Error")
		return false, metricsErr
	}
	players, playersErr := GetAPIPlayers(cfg.RestAPI.Port, pw)
	settings, settingsErr := GetAPISettings(cfg.RestAPI.Port, pw)
	if metricsErr != nil || playersErr != nil || settingsErr != nil {
		LogWarn.Printf("Health check failed. Errors: %v, %v, %v", metricsErr, playersErr, settingsErr)
		state.SetStatus("Unhealthy")
		return false, nil
	}
	LogSuccess.Printf("Health check successful. FPS: %d, Players: %d/%d", metrics.ServerFPS, metrics.PlayerCount, metrics.MaxPlayers)
	state.UpdateFullState("Healthy", settings.ServerName, players.Players, metrics.MaxPlayers, metrics.ServerFPS, metrics.Uptime)
	return true, nil
}