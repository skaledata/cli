package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

// ConfigDir returns the path to ~/.config/skaledata, creating it if needed.
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "skaledata")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// ConfigPath returns the full path to config.yaml.
func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// SaveToken writes the auth token to config.yaml.
func SaveToken(token string) error {
	return saveField("token", token)
}

// SaveAPIKey writes the API key to config.yaml.
func SaveAPIKey(key string) error {
	return saveField("api_key", key)
}

// GetAuthHeader returns the Authorization header value.
// Prefers API key over token.
func GetAuthHeader() (string, error) {
	if key := viper.GetString("api_key"); key != "" {
		return "Bearer " + key, nil
	}
	if token := viper.GetString("token"); token != "" {
		return "Bearer " + token, nil
	}
	return "", fmt.Errorf("not authenticated — run 'skaledata login' or 'skaledata auth set-key'")
}

// APIURL returns the configured API base URL.
func APIURL() string {
	return viper.GetString("api_url")
}

func saveField(key, value string) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	// Read existing config
	data := make(map[string]interface{})
	if existing, err := os.ReadFile(path); err == nil {
		_ = yaml.Unmarshal(existing, &data)
	}

	data[key] = value

	out, err := yaml.Marshal(data)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}
