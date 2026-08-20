package guild

import "sync"

var (
	locks    = make(map[string]*sync.Mutex)
	locksMux sync.Mutex
)

func AcquireLock(guildID string) *sync.Mutex {
	locksMux.Lock()
	defer locksMux.Unlock()

	if lock, exists := locks[guildID]; exists {
		return lock
	}

	lock := &sync.Mutex{}
	locks[guildID] = lock
	return lock
}
