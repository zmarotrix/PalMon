package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"
)

// --- Authentication Middleware ---
func authMiddleware(next http.Handler, cfg *Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != cfg.Web.Username || pass != cfg.Web.Password {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("Unauthorized.\n"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Web Server Setup ---
func startWebServer(cfg *Config) {
	mux := http.NewServeMux()
	tpl := template.Must(template.ParseFiles("templates/index.html"))

	// --- Public Endpoint ---
	mux.HandleFunc("/api/publicinfo", handlePublicInfo(cfg))

	// --- Admin-Only Endpoints ---
	mux.Handle("/api/authcheck", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }), cfg))
	mux.Handle("/actions/kick", authMiddleware(http.HandlerFunc(handlePlayerAction("kick", cfg)), cfg))
	mux.Handle("/actions/ban", authMiddleware(http.HandlerFunc(handlePlayerAction("ban", cfg)), cfg))
	mux.Handle("/actions/broadcast", authMiddleware(http.HandlerFunc(handleBroadcast(cfg)), cfg))
	mux.Handle("/actions/restart", authMiddleware(http.HandlerFunc(handleServerAction("restart")), cfg))
	mux.Handle("/actions/shutdown", authMiddleware(http.HandlerFunc(handleServerAction("shutdown")), cfg))
	mux.Handle("/actions/refresh", authMiddleware(http.HandlerFunc(handleRefresh()), cfg))
	
	// --- Root handler for the HTML page ---
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

	log.Printf("Web interface listening on http://%s", cfg.Web.ListenAddress)
	if err := http.ListenAndServe(cfg.Web.ListenAddress, mux); err != nil {
		log.Fatalf("Failed to start web server: %v", err)
	}
}

// --- Handler for Public Info ---
func handlePublicInfo(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, serverName, players, maxPlayers, fps, uptime := state.GetFullState()
		playerStr := "N/A"
		if status == "Healthy" {
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

// --- Handlers for Admin Actions ---
func handlePlayerAction(action string, cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload struct{ UserID string `json:"userId"` }
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		var err error
		if action == "kick" {
			err = PostKickPlayer(cfg.RestAPI.Port, cfg.RCON.Password, payload.UserID)
		} else if action == "ban" {
			err = PostBanPlayer(cfg.RestAPI.Port, cfg.RCON.Password, payload.UserID)
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
		if err := PostBroadcast(cfg.RestAPI.Port, cfg.RCON.Password, payload.Message); err != nil {
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
		ForceCheckChan <- responseChan // Send the request to the monitor loop
		<-responseChan                 // Wait until the monitor loop signals completion
		w.WriteHeader(http.StatusOK)
	}
}

// --- Helper Function ---
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