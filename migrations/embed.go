// Package migrations embeds SQL migrations: app/ is applied by api,
// mocks/ by mcp-mocks (12 §8.1). Each chain has its own version table.
package migrations

import (
	"embed"
	"io/fs"
)

//go:embed app/*.sql mocks/*.sql
var files embed.FS

// App returns migrations of the app schema.
func App() fs.FS { sub, _ := fs.Sub(files, "app"); return sub }

// Mocks returns migrations of the mock_* schemas.
func Mocks() fs.FS { sub, _ := fs.Sub(files, "mocks"); return sub }
