package main

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Locates the active PalWorldSettings.ini file automatically
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
	return "", fmt.Errorf("PalWorldSettings.ini not found in WindowsServer or LinuxServer folders")
}

// Reads the INI and rips the OptionSettings string into a Go map
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
	
	// Split by comma, then by equals sign
	parts := strings.Split(settingsStr, ",")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			settings[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return settings, nil
}

// Backs up the old INI and writes the new settings
func SaveINI(basePath string, settings map[string]string) error {
	path, err := getSettingsPath(basePath)
	if err != nil {
		return err
	}

	// Create Backup
	backupPath := fmt.Sprintf("%s.bak.%d", path, time.Now().Unix())
	if err := copyFile(path, backupPath); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	// Reconstruct the weird comma-separated string
	var parts []string
	for k, v := range settings {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	newOptions := fmt.Sprintf("OptionSettings=(%s)", strings.Join(parts, ","))

	// Write the final INI format
	newContent := fmt.Sprintf("[/Script/Pal.PalGameWorldSettings]\n%s\n", newOptions)
	return os.WriteFile(path, []byte(newContent), 0644)
}

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

// EnsureSettings verifies the INI exists, enforces RESTAPIEnabled=True,
// secures a blank AdminPassword, and returns the configured Port and Password.
func EnsureSettings(basePath string) (int, string, error) {
	destPath, err := getSettingsPath(basePath)
	if err != nil || destPath == "" {
		destPath = filepath.Join(basePath, "Pal", "Saved", "Config", "WindowsServer", "PalWorldSettings.ini")
	}

	// 1. Check if the file is blank or missing
	info, err := os.Stat(destPath)
	needsDefault := os.IsNotExist(err) || (err == nil && info.Size() < 50)

	if needsDefault {
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return 0, "", fmt.Errorf("failed to create config directory: %w", err)
		}
		defaultPath := filepath.Join(basePath, "DefaultPalWorldSettings.ini")
		if err := copyFile(defaultPath, destPath); err != nil {
			return 0, "", fmt.Errorf("failed to copy default settings: %w", err)
		}
		fmt.Printf("INFO: Copied default settings from %s\n", defaultPath)
	}

	// 2. Read the file
	data, err := os.ReadFile(destPath)
	if err != nil {
		return 0, "", fmt.Errorf("failed to read settings file: %w", err)
	}
	
	content := string(data)
	changed := false

	// 3. Safely enable REST API
	reEnable := regexp.MustCompile(`RESTAPIEnabled=[a-zA-Z]+`)
	if reEnable.MatchString(content) {
		if !strings.Contains(content, "RESTAPIEnabled=True") {
			content = reEnable.ReplaceAllString(content, "RESTAPIEnabled=True")
			changed = true
		}
	} else if strings.Contains(content, "OptionSettings=(") {
		content = strings.Replace(content, ")", ",RESTAPIEnabled=True)", 1)
		changed = true
	}

	// 4. Extract the RESTAPIPort dynamically
	restPort := 8212 // Default fallback
	rePort := regexp.MustCompile(`RESTAPIPort=(\d+)`)
	match := rePort.FindStringSubmatch(content)
	
	if len(match) > 1 {
		if parsedPort, err := strconv.Atoi(match[1]); err == nil {
			restPort = parsedPort
		}
	} else if strings.Contains(content, "OptionSettings=(") {
		content = strings.Replace(content, ")", ",RESTAPIPort=8212)", 1)
		changed = true
	}

	// 5. Ensure AdminPassword is not blank
	adminPass := ""
	reAdminPass := regexp.MustCompile(`AdminPassword="([^"]*)"`)
	adminPassMatch := reAdminPass.FindStringSubmatch(content)

	if len(adminPassMatch) > 1 {
		adminPass = adminPassMatch[1]
	}

	if adminPass == "" {
		// Generate a random 5-digit number (between 10000 and 99999)
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		adminPass = fmt.Sprintf("BlankPassFix-%d", r.Intn(90000)+10000)

		if len(adminPassMatch) > 1 {
			// Replace the empty quotes with our new password
			content = reAdminPass.ReplaceAllString(content, `AdminPassword="`+adminPass+`"`)
		} else if strings.Contains(content, "OptionSettings=(") {
			// Fallback if missing entirely from the string
			content = strings.Replace(content, ")", `,AdminPassword="`+adminPass+`")`, 1)
		}
		changed = true
		
		fmt.Printf("\n======================================================\n")
		fmt.Printf("WARNING: AdminPassword was blank in PalWorldSettings.ini!\n")
		fmt.Printf("It has been automatically secured and set to: %s\n", adminPass)
		fmt.Printf("Please update your config.json if you want a custom one.\n")
		fmt.Printf("======================================================\n\n")
	}

	// 6. Save only if we modified the file
	if changed {
		if err := os.WriteFile(destPath, []byte(content), 0644); err != nil {
			return 0, "", fmt.Errorf("failed to save updated settings: %w", err)
		}
		fmt.Println("INFO: Automatically enforced required settings in PalWorldSettings.ini")
	}

	return restPort, adminPass, nil
}