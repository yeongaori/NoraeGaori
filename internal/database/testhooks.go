//go:build testhooks

package database

var HookCreateTables = createTables
var HookRunMigrations = runMigrations
