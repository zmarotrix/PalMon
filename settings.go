package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func getSettingsPath(basePath string) (string, error) {
	paths := []string{
		filepath.Join(basePath, "Pal", "Saved", "Config", "WindowsServer", "PalWorldSettings.ini"),
		filepath.Join(basePath, "Pal", "Saved", "Config", "LinuxServer", "PalWorldSettings.ini"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("PalWorldSettings.ini not found")
}

// THIS IS THE NEW, CORRECTED PARSER
func ParseINI(basePath string) (map[string]string, error) {
	path, err := getSettingsPath(basePath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	re := regexp.MustCompile(`OptionSettings=\((.*)\)`)
	matches := re.FindStringSubmatch(string(data))
	if len(matches) < 2 {
		return nil, fmt.Errorf("OptionSettings string not found in file")
	}
	settingsStr := matches[1]
	settings := make(map[string]string)
	
	// --- THIS IS THE FIX ---
	// 1. Isolate the special CrossplayPlatforms setting first.
	crossplayRe := regexp.MustCompile(`CrossplayPlatforms=\([^)]*\)`)
	crossplayMatch := crossplayRe.FindString(settingsStr)
	if crossplayMatch != "" {
		kv := strings.SplitN(crossplayMatch, "=", 2)
		settings[kv[0]] = kv[1]
		// 2. Remove it from the main string so we can parse the rest safely.
		settingsStr = crossplayRe.ReplaceAllString(settingsStr, "")
	}
	// --- END OF FIX ---

	// 3. Now parse the rest of the simple key=value pairs.
	parts := strings.Split(settingsStr, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			settings[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return settings, nil
}


func SaveINI(basePath string, settings map[string]string) error {
	path, err := getSettingsPath(basePath)
	if err != nil {
		path = filepath.Join(basePath, "Pal", "Saved", "Config", "WindowsServer", "PalWorldSettings.ini")
	}

	if _, err := os.Stat(path); err == nil {
		backupPath := fmt.Sprintf("%s.bak.%d", path, time.Now().Unix())
		if err := copyFile(path, backupPath); err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
	}

	var keys []string
	for k := range settings {
		// We handle the special case separately
		if k != "CrossplayPlatforms" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, settings[k]))
	}
	
	// Add the special crossplay setting at the end, if it exists
	if crossplayVal, ok := settings["CrossplayPlatforms"]; ok {
		parts = append(parts, fmt.Sprintf("CrossplayPlatforms=%s", crossplayVal))
	}

	newOptions := fmt.Sprintf("OptionSettings=(%s)", strings.Join(parts, ","))
	newContent := fmt.Sprintf("[/Script/Pal.PalGameWorldSettings]\n%s\n", newOptions)
	return os.WriteFile(path, []byte(newContent), 0644)
}

func EnsureSettings(basePath string) (int, string, error) {
	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		return 0, "", fmt.Errorf("the server path '%s' does not exist", basePath)
	}

	_, err := getSettingsPath(basePath)
	if err != nil {
		LogWarn.Println("PalWorldSettings.ini not found. Attempting to create a new one from default.")
		destPath := filepath.Join(basePath, "Pal", "Saved", "Config", "WindowsServer", "PalWorldSettings.ini")
		defaultPath := filepath.Join(basePath, "DefaultPalWorldSettings.ini")

		if _, err := os.Stat(defaultPath); os.IsNotExist(err) {
			return 0, "", fmt.Errorf("CRITICAL: DefaultPalWorldSettings.ini is missing from your server directory. Please verify the server files in Steam to restore it")
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return 0, "", fmt.Errorf("failed to create config directory: %w. Try running as Administrator", err)
		}
		if err := copyFile(defaultPath, destPath); err != nil {
			return 0, "", fmt.Errorf("failed to copy default settings: %w. Try running as Administrator", err)
		}
		LogSuccess.Printf("Created a new PalWorldSettings.ini from the default template.")
	}

	settingsMap, err := ParseINI(basePath)
	if err != nil {
		return 0, "", fmt.Errorf("failed to parse settings file: %w", err)
	}

	changed := false

	if val, ok := settingsMap["RESTAPIEnabled"]; !ok || val != "True" {
		settingsMap["RESTAPIEnabled"] = "True"
		changed = true
	}

	// --- THIS IS THE CRITICAL FIX ---
	// We now correctly look for RESTAPIPort and default to 8212.
	apiPort := 8212 
	if val, ok := settingsMap["RESTAPIPort"]; ok {
		if p, err := strconv.Atoi(val); err == nil {
			apiPort = p
		}
	} else {
		// If RESTAPIPort is missing, we add it and default to 8212.
		settingsMap["RESTAPIPort"] = "8212"
		changed = true
	}
	// --- END OF FIX ---

	adminPass, ok := settingsMap["AdminPassword"]
	adminPass = strings.Trim(adminPass, `"`)
	if !ok || adminPass == "" {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		adminPass = fmt.Sprintf("BlankPassFix-%d", r.Intn(90000)+10000)
		settingsMap["AdminPassword"] = fmt.Sprintf(`"%s"`, adminPass)
		changed = true
		fmt.Printf("\n======================================================\n")
		fmt.Printf("WARNING: AdminPassword was blank in PalWorldSettings.ini!\n")
		fmt.Printf("A secure password has been generated for you: %s\n", adminPass)
		fmt.Printf("======================================================\n\n")
	}

	if changed {
		if err := SaveINI(basePath, settingsMap); err != nil {
			return 0, "", fmt.Errorf("failed to save updated settings: %w", err)
		}
		LogInfo.Println("Automatically configured required settings in PalWorldSettings.ini")
	}

	return apiPort, adminPass, nil
}


// --- All other functions below this line are unchanged ---

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil { return err }
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil { return err }
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func getPalMonSettingsPath() string {
	return "PalMonServerSettings.json"
}

func LoadPalMonSettings() (map[string]string, error) {
	path := getPalMonSettingsPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("PalMon settings file does not exist yet")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var settings map[string]string
	err = json.Unmarshal(data, &settings)
	return settings, err
}

func SavePalMonSettings(settings map[string]string) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(getPalMonSettingsPath(), data, 0644)
}