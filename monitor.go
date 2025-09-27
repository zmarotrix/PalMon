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

// ServerState and its methods are unchanged
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

// forceKillProcess uses the robust taskkill command to terminate the process.
func forceKillProcess(pid int) error {
	LogWarn.Printf("Executing forceful taskkill on PID: %d", pid)
	cmd := exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid))
	return cmd.Run()
}

// isProcessRunning function is unchanged
func isProcessRunning(pid int) bool {
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid))
	output, err := cmd.Output()
	if err != nil { return false }
	return strings.Contains(string(output), strconv.Itoa(pid))
}

func runMonitor(cfg *Config) {
	for {
		LogInfo.Println("--- STARTING SERVER ---")
		state.UpdateFullState("Starting", "N/A", nil, 0, 0, 0)

		cmd := exec.Command(filepath.Join(cfg.Server.Path, cfg.Server.Executable)); cmd.Dir = cfg.Server.Path
		if err := cmd.Start(); err != nil { LogError.Fatalf("Failed to start server: %v", err) }
		serverProcess = cmd.Process
		LogInfo.Printf("Server process started with PID: %d", serverProcess.Pid)
		sendDiscordNotification(cfg.Discord.WebhookURL, "Server process started.")

		processDone := make(chan error, 1); go func() { processDone <- cmd.Wait() }()
		
		wasResponsive := monitorHealth(cfg, processDone)

		// --- NEW TERMINATION LOGIC ---
		if serverProcess != nil {
			pid := serverProcess.Pid
			if isProcessRunning(pid) {
				if !wasResponsive { // If it was a crash/hang, use the hammer
					LogError.Printf("Server process %d is unresponsive. Forcing termination.", pid)
					forceKillProcess(pid)
				} else { // If it was an admin action or clean exit, try graceful first
					LogWarn.Printf("Process %d stopping. Attempting graceful shutdown.", pid)
					PostShutdown(cfg.RestAPI.Port, cfg.RCON.Password)
					time.Sleep(5 * time.Second) // Give it a moment
				}

				// Confirmation Loop: Do not proceed until the process is gone.
				LogInfo.Printf("Waiting for process %d to terminate...", pid)
				for i := 0; i < 15; i++ { // Max wait of 15 seconds
					if !isProcessRunning(pid) {
						LogSuccess.Printf("Process %d terminated successfully.", pid)
						break
					}
					time.Sleep(1 * time.Second)
				}
				// If it's *still* running, try the hammer one last time
				if isProcessRunning(pid) {
					LogError.Printf("Process %d failed to terminate. Using final taskkill.", pid)
					forceKillProcess(pid)
				}
			}
		}

		<-processDone // Wait for the original process handle to be released.

		if !wasResponsive { sendDiscordNotification(cfg.Discord.WebhookURL, "Server became unresponsive and is being restarted.")
		} else { sendDiscordNotification(cfg.Discord.WebhookURL, "Server process exited and is being restarted.") }
		
		state.SetStatus("Restarting"); LogWarn.Println("Restarting in 10 seconds..."); time.Sleep(10 * time.Second)
	}
}

func runHealthCheck(cfg *Config) bool {
	metrics, metricsErr := GetAPIMetrics(cfg.RestAPI.Port, cfg.RCON.Password)
	players, playersErr := GetAPIPlayers(cfg.RestAPI.Port, cfg.RCON.Password)
	settings, settingsErr := GetAPISettings(cfg.RestAPI.Port, cfg.RCON.Password)
	if metricsErr != nil || playersErr != nil || settingsErr != nil {
		LogWarn.Printf("Health check failed. Errors: %v, %v, %v", metricsErr, playersErr, settingsErr)
		state.SetStatus("Unhealthy")
		return false // Check failed
	}
	LogSuccess.Printf("Health check successful. FPS: %d, Players: %d/%d", metrics.ServerFPS, metrics.PlayerCount, metrics.MaxPlayers)
	state.UpdateFullState("Healthy", settings.ServerName, players.Players, metrics.MaxPlayers, metrics.ServerFPS, metrics.Uptime)
	return true // Check succeeded
}

func monitorHealth(cfg *Config, processDone chan error) (wasResponsive bool) {
	LogInfo.Printf("Waiting %d seconds for server to initialize...", cfg.Monitor.StartupDelaySec)
	state.SetStatus(fmt.Sprintf("Initializing (%ds)", cfg.Monitor.StartupDelaySec))
	select {
	case err := <-processDone: LogWarn.Printf("Server process exited during startup: %v", err); return true
	case <-time.After(time.Duration(cfg.Monitor.StartupDelaySec) * time.Second): LogInfo.Println("Initial startup delay complete.")
	}

	healthTicker := time.NewTicker(time.Duration(cfg.Monitor.CheckIntervalSec) * time.Second); defer healthTicker.Stop()
	
	for {
		select {
		case <-healthTicker.C:
			if !runHealthCheck(cfg) {
				// --- NEW RAPID TRIAGE LOGIC ---
				LogWarn.Println("Entering rapid triage state...")
				state.SetStatus("Confirming")
				isRecovered := false
				for i := 1; i < cfg.Monitor.UnhealthyThreshold; i++ {
					time.Sleep(time.Duration(cfg.Monitor.RapidCheckIntervalSec) * time.Second)
					LogInfo.Printf("Performing rapid check %d/%d...", i+1, cfg.Monitor.UnhealthyThreshold)
					if runHealthCheck(cfg) {
						LogSuccess.Println("Server recovered during triage.")
						isRecovered = true
						break
					}
				}
				if !isRecovered {
					LogError.Println("Server failed all triage checks and is confirmed unresponsive.")
					return false // Unresponsive
				}
			}
		case respChan := <-ForceCheckChan:
			LogInfo.Println("Manual health check triggered by admin.")
			runHealthCheck(cfg); healthTicker.Reset(time.Duration(cfg.Monitor.CheckIntervalSec) * time.Second); respChan <- true
		case err := <-processDone: LogWarn.Printf("Server process exited: %v", err); return true
		case action := <-AdminActionChan:
			LogInfo.Printf("Received admin action: %s", action); return true
		}
	}
}