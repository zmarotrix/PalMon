package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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