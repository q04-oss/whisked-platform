// Package migrations embeds the SQL migration files so they are bundled into
// the server binary. No filesystem access is needed at runtime.
package migrations

import "embed"

//go:embed *.up.sql
var FS embed.FS
