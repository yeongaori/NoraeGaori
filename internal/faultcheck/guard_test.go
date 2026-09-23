//go:build faults

package faultcheck

import (
	"fmt"
	"runtime/debug"
	"strings"
	"time"
)

type Outcome struct {
	Err       error
	Panic     any
	Location  string
	IsTimeout bool
}

func Guard(timeout time.Duration, fire func() error) Outcome {
	done := make(chan Outcome, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				done <- Outcome{Panic: recovered, Location: panicLocation(debug.Stack())}
			}
		}()
		done <- Outcome{Err: fire()}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case outcome := <-done:
		return outcome
	case <-timer.C:
		return Outcome{IsTimeout: true}
	}
}

func (outcome Outcome) Crash() string {
	switch {
	case outcome.Panic != nil:
		return fmt.Sprintf("panicked: %v at %s", outcome.Panic, outcome.Location)
	case outcome.IsTimeout:
		return "did not finish in time"
	}
	return ""
}

func panicLocation(stack []byte) string {
	lines := strings.Split(string(stack), "\n")
	for index := 0; index+1 < len(lines); index++ {
		function := lines[index]
		if strings.Contains(function, "noraegaori/internal/") && !strings.Contains(function, "noraegaori/internal/faultcheck.") {
			return strings.TrimSpace(lines[index+1])
		}
	}
	return "an unknown location"
}
