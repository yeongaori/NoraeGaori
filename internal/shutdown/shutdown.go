package shutdown

import "sync/atomic"

var shuttingDown atomic.Bool

// SetShuttingDown marks the bot as shutting down
func SetShuttingDown() {
	shuttingDown.Store(true)
}

// IsShuttingDown returns true if the bot is in the process of shutting down
func IsShuttingDown() bool {
	return shuttingDown.Load()
}
