package dependency

import (
	"errors"
	"fmt"
	"runtime"

	"noraegaori/internal/dependency/mirror"
	"noraegaori/internal/logger"
)

func currentPlatform() mirror.Platform {
	return mirror.Platform{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
}

type tool struct {
	name        string
	displayName string
	versionFlag string
	minimum     string
	maximum     string
	mirrors     []mirror.Mirror
}

func (t *tool) accepts(version string) bool {
	if t.minimum != "" && compareVersions(version, t.minimum) < 0 {
		return false
	}
	if t.maximum != "" && compareVersions(version, t.maximum) > 0 {
		return false
	}
	return true
}

type outcome int

const (
	outcomeNoBuild outcome = iota
	outcomeUnreachable
	outcomeInstallFailed
)

type probeResult struct {
	tool    *tool
	outcome outcome
	err     error
}

func (result probeResult) describe(target mirror.Platform) string {
	switch result.outcome {
	case outcomeUnreachable:
		return fmt.Sprintf("%s mirrors are unreachable (%v)", result.tool.displayName, result.err)
	case outcomeInstallFailed:
		return fmt.Sprintf("%s failed to install (%v)", result.tool.displayName, result.err)
	default:
		return fmt.Sprintf("%s has no build for %s", result.tool.displayName, target)
	}
}

type acceptFunc func(t *tool, found *mirror.Candidate) (*Binary, error)

func probeTool(t *tool, target mirror.Platform, accept acceptFunc) (*Binary, probeResult) {
	result := probeResult{tool: t, outcome: outcomeNoBuild}

	for _, m := range t.mirrors {
		found, err := m.Find(target, t.accepts)
		if errors.Is(err, mirror.ErrNoBuild) {
			logger.Debugf("%s: %s publishes no build for %s", t.name, m.Name(), target)
			continue
		}
		if err != nil {
			logger.Debugf("%s: %s could not be checked: %v", t.name, m.Name(), err)
			if result.outcome == outcomeNoBuild {
				result = probeResult{tool: t, outcome: outcomeUnreachable, err: err}
			}
			continue
		}

		binary, err := accept(t, found)
		if err == nil {
			return binary, probeResult{}
		}
		logger.Debugf("%s %s from %s failed to install: %v", t.name, found.Version, m.Name(), err)
		result = probeResult{tool: t, outcome: outcomeInstallFailed, err: err}
	}

	return nil, result
}

func discover(tools []*tool, target mirror.Platform, accept acceptFunc) (*Binary, []probeResult) {
	results := make([]probeResult, 0, len(tools))
	for _, t := range tools {
		binary, result := probeTool(t, target, accept)
		if binary != nil {
			return binary, results
		}
		results = append(results, result)
	}
	return nil, results
}
