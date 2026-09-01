package guild

import "noraegaori/internal/lockmap"

var guildLocks lockmap.Map

func AcquireLock(guildID string) func() {
	return guildLocks.Acquire(guildID)
}
