// Command ssh-farm is Idle Farmer v2: the ssharcade-native rebuild of
// ssh-idlefarmer with mouse support, leaderboards, and durable off-instance
// player data. Running it with no arguments starts the SSH server; `ssh-farm
// import-v1 --from <path>` runs the one-time v1 database migration. See
// docs/README.md for the architecture and per-task specs.
package main

import "os"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "import-v1" {
		runImportV1(os.Args[2:])
		return
	}
	runServe()
}
