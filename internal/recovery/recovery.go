package recovery

import (
	"fmt"
	"runtime/debug"

	"noraegaori/internal/logger"
)

func Run(name string, fn func() error) (err error) {
	defer func() {
		if reason := recover(); reason != nil {
			logger.Errorf("%s panicked: %v\n%s", name, reason, debug.Stack())
			err = fmt.Errorf("%s panicked", name)
		}
	}()
	return fn()
}

func Go(name string, fn func()) {
	go func() {
		_ = Run(name, func() error {
			fn()
			return nil
		})
	}()
}
