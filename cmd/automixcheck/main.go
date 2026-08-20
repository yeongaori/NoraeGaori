//go:build automixcheck

package main

import (
	"fmt"
	"os"

	"noraegaori/internal/commands/automix"
	"noraegaori/internal/messages"
	"noraegaori/internal/player"
)

func main() {
	if err := messages.LoadLocale("en"); err != nil {
		fmt.Printf("failed to load locale: %v\n", err)
		os.Exit(1)
	}

	results := player.RunAutoMixChecks()
	results = append(results, automix.RunAutoMixPanelChecks()...)

	passed := 0
	failed := 0
	for _, result := range results {
		marker := "PASS"
		if !result.Passed {
			marker = "FAIL"
			failed++
		} else {
			passed++
		}
		fmt.Printf("[%s] %s\n       %s\n", marker, result.Name, result.Detail)
	}

	fmt.Printf("\n%d passed, %d failed, %d total\n", passed, failed, len(results))
	if failed > 0 {
		os.Exit(1)
	}
}
