package migrations

import _ "embed"

// CoreSQL is the V0.8 key-table contract. M1 uses the file, build, lexical, and query tables.
//
//go:embed 001_core.sql
var CoreSQL string
