// Package data embeds the game content files so the compiled binary is
// self-contained. Operators can still override them at runtime by pointing
// FARM_DATA_DIR at a directory with replacement TOML files.
package data

import "embed"

//go:embed *.toml
var FS embed.FS
