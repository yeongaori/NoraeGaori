//go:build faults

package faultcheck

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
)

type Failure struct {
	Step     string
	Trigger  string
	Scenario string
	Detail   string
}

type Report struct {
	mu       sync.Mutex
	passes   int
	failures []Failure
}

func (report *Report) Pass() {
	report.mu.Lock()
	defer report.mu.Unlock()
	report.passes++
}

func (report *Report) Fail(failure Failure) {
	report.mu.Lock()
	defer report.mu.Unlock()
	report.failures = append(report.failures, failure)
}

func (report *Report) HasFailures() bool {
	report.mu.Lock()
	defer report.mu.Unlock()
	return len(report.failures) > 0
}

func (report *Report) Print(w io.Writer) error {
	report.mu.Lock()
	failures := slices.Clone(report.failures)
	passes, total := report.passes, len(report.failures)
	report.mu.Unlock()

	slices.SortStableFunc(failures, compareFailures)
	for index := range failures {
		failure := &failures[index]
		if _, err := fmt.Fprintf(w, "FAIL  %s | %s | %s\n      %s\n", failure.Step, failure.Scenario, failure.Trigger, failure.Detail); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "%d checks passed, %d failed\n", passes, total)
	return err
}

func compareFailures(first, second Failure) int {
	return cmp.Or(
		strings.Compare(first.Step, second.Step),
		strings.Compare(first.Scenario, second.Scenario),
		strings.Compare(first.Trigger, second.Trigger),
	)
}
