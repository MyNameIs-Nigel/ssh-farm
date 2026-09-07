// Package data embeds the game content files so the compiled binary is
// self-contained. Operators can still override them at runtime by pointing
// FARM_DATA_DIR at a directory with replacement TOML files.
//
// moderation/ holds namewords.toml only. The denylist used to live beside it
// and no longer does: it is host state loaded from FARM_MODERATION_PATH (see
// internal/moderation and docs/gameplay/03-farm-names-and-moderation.md),
// because a public repository cannot carry a private list. namewords.toml is
// wholesome by construction and stays embedded so a clone works out of the box.
package data

import "embed"

//go:embed *.toml moderation/*.toml
var FS embed.FS
