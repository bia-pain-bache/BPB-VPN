//go:build tools

// Keeps gomobile in go.mod, so scripts/build_core.sh builds with the pinned version.
package xray

import (
	_ "golang.org/x/mobile/bind"
	_ "golang.org/x/mobile/cmd/gobind"
	_ "golang.org/x/mobile/cmd/gomobile"
)
