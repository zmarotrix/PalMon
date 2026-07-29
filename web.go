package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// --- Authentication Middleware ---
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg, ok := r.Context().Value("config").(*Config)
		if !ok {
			log.Println("Error: config not found in context")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		user, pass, ok := r.BasicAuth()
		if !ok || user != cfg.Web.Username || pass != cfg.Web.Password {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("Unauthorized.\n"))
			return
		}
		next(w, r)
	}
}

// --- Web Server Setup ---
func startWebServer(cfg *Config, errChan chan error) {
	mux := http.NewServeMux()

	// Public API endpoint
	mux.HandleFunc("/api/publicinfo", handlePublicInfo(cfg))

	// Admin API endpoints (all protected by the auth middleware)
	mux.HandleFunc("/api/authcheck", authMiddleware(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	mux.HandleFunc("/actions/kick", authMiddleware(handlePlayerAction("kick", cfg)))
	mux.HandleFunc("/actions/ban", authMiddleware(handlePlayerAction("ban", cfg)))
	mux.HandleFunc("/actions/broadcast", authMiddleware(handleBroadcast(cfg)))
	mux.HandleFunc("/actions/restart", authMiddleware(handleServerAction("restart")))
	mux.HandleFunc("/actions/shutdown", authMiddleware(handleServerAction("shutdown")))
	mux.HandleFunc("/actions/start", authMiddleware(handleServerAction("start")))
	mux.HandleFunc("/actions/refresh", authMiddleware(handleRefresh()))
	mux.HandleFunc("/api/settings_file", authMiddleware(handleGetSettings(cfg)))
	mux.HandleFunc("/actions/settings_save", authMiddleware(handleSaveSettings(cfg)))
	
	// Root handler for the HTML page
	tpl := template.Must(template.ParseFiles("templates/index.html"))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if err := tpl.Execute(w, nil); err != nil {
			log.Printf("Error executing template: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	// Create a handler that injects the config into the request context for the middleware
	handlerWithConfig := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = context.WithValue(ctx, "config", cfg)
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
	

	parts := strings.Split(cfg.Web.ListenAddress, ":")
	port := parts[len(parts)-1]

	LogInfo.Printf("Web interface bound to %s", cfg.Web.ListenAddress)
	LogInfo.Printf("👉 Local Web UI Link: http://localhost:%s", port)
	LogInfo.Println("Waiting for Palworld server to fully boot before opening browser...")

	localURL := "http://localhost:" + port
	
	go func() {
		for {
			status, _, _, _, _, _ := state.GetFullState()
			
			if status == "Healthy" || status == "Config Error" {
				if status == "Healthy" {
					LogInfo.Println("Server is ready! Auto-opening Web UI...")
				} else {
					LogWarn.Println("Configuration error detected! Opening Web UI so you can fix it...")
				}

				var browserErr error
				browserErr = exec.Command("rundll32", "url.dll,FileProtocolHandler", localURL).Start()

				if browserErr != nil {
					LogWarn.Printf("Could not auto-open browser, but you can click the link above.")
				}
				break // Exit the loop once the browser has been opened
			}
			
			// Wait 2 seconds before checking the server state again
			time.Sleep(2 * time.Second)
		}
	}()
    
	if err := http.ListenAndServe(cfg.Web.ListenAddress, handlerWithConfig); err != nil {
		errChan <- fmt.Errorf("failed to start web server: %w", err)
	}
}

// --- Handlers ---
func handlePublicInfo(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, serverName, players, maxPlayers, fps, uptime := state.GetFullState()
		playerStr := "N/A"
		if status != "Unhealthy" && status != "Confirming" {
			playerStr = fmt.Sprintf("%d / %d", len(players), maxPlayers)
		}
		if serverName == "" {
			serverName = cfg.Web.PageTitle
		}
		data := map[string]interface{}{
			"Status":       status,
			"ServerName":   serverName,
			"PlayerString": playerStr,
			"Players":      players,
			"ServerFPS":    fps,
			"UptimeString": formatUptime(uptime),
			"LastChecked":  time.Now().Format("15:04:05 MST"),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	}
}

func handlePlayerAction(action string, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload struct{ UserID string `json:"userId"` }
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		var err error
		if action == "kick" {
			err = PostKickPlayer(cfg.RestAPI.Port, cfg.GetAdminPassword(), payload.UserID)
		} else if action == "ban" {
			err = PostBanPlayer(cfg.RestAPI.Port, cfg.GetAdminPassword(), payload.UserID)
		}
		if err != nil {
			log.Printf("Failed to %s player %s: %v", action, payload.UserID, err)
			http.Error(w, "Action failed", http.StatusInternalServerError)
		} else {
			log.Printf("Player %s %sed successfully", payload.UserID, action)
			w.WriteHeader(http.StatusOK)
		}
	}
}

func handleBroadcast(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload struct{ Message string `json:"message"` }
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		if err := PostBroadcast(cfg.RestAPI.Port, cfg.GetAdminPassword(), payload.Message); err != nil {
			log.Printf("Broadcast failed: %v", err)
			http.Error(w, "Broadcast failed", http.StatusInternalServerError)
		} else {
			log.Println("Broadcast sent.")
			w.WriteHeader(http.StatusOK)
		}
	}
}

func handleServerAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		AdminActionChan <- action
		w.WriteHeader(http.StatusOK)
	}
}

func handleRefresh() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		responseChan := make(chan bool)
		ForceCheckChan <- responseChan
		<-responseChan
		w.WriteHeader(http.StatusOK)
	}
}

func formatUptime(s uint64) string {
	d := time.Duration(s) * time.Second
	d = d.Round(time.Second)
	days := d / (24 * time.Hour)
	d -= days * 24 * time.Hour
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, h, m)
	}
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func handleGetSettings(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings, err := ParseINI(cfg.Server.Path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(settings)
	}
}

func handleSaveSettings(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var settings map[string]string
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		if err := SaveINI(cfg.Server.Path, settings); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		LogSuccess.Println("Admin successfully updated PalWorldSettings.ini and created a backup.")
		w.WriteHeader(http.StatusOK)
	}
}