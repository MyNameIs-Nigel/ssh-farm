// Command dev-farm starts a loopback SSH server with isolated saves and Ctrl+D tools.
package main

import (
	"os"

	"github.com/mynameis-nigel/ssh-farm/internal/app"
)

func main() {
	setDefaults()
	app.Run()
}

func setDefaults() {
	for key, value := range map[string]string{
		"FARM_LISTEN_HOST":   "127.0.0.1",
		"FARM_LISTEN_PORT":   "2222",
		"FARM_DB_PATH":       "var/dev/farm-dev.db",
		"FARM_HOST_KEY_PATH": "var/dev/ssh_host_key",
		"FARM_LOG_LEVEL":     "debug",
		"FARM_DEV_MODE":      "1",
		"FARM_IDLE_TIMEOUT":  "0",
	} {
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
}
