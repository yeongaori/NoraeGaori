package dependency

import (
	"fmt"

	"noraegaori/internal/dependency/mirror"
	"noraegaori/internal/logger"
)

var jsRuntimes = []*tool{
	{
		name:        "deno",
		displayName: "Deno",
		versionFlag: "--version",
		minimum:     "2.3.0",
		mirrors: []mirror.Mirror{
			mirror.DenoGitHub{APIURL: mirror.GitHubAPI},
			mirror.DenoCDN{BaseURL: "https://dl.deno.land"},
		},
	},
	{
		name:        "node",
		displayName: "Node.js",
		versionFlag: "--version",
		minimum:     "22.0.0",
		mirrors:     []mirror.Mirror{mirror.NodeDist{BaseURL: "https://nodejs.org/dist"}},
	},
	{
		name:        "bun",
		displayName: "Bun",
		versionFlag: "--version",
		minimum:     "1.2.11",
		maximum:     "1.3.14",
		mirrors:     []mirror.Mirror{mirror.BunGitHub{APIURL: mirror.GitHubAPI}},
	},
}

func JsRuntimeArg() string {
	binary := jsRuntimeSlot.current()
	if binary == nil {
		return ""
	}
	if binary.isSystem {
		return binary.Tool
	}
	return binary.Tool + ":" + binary.Path
}

func findJsRuntime(name string) (*tool, int) {
	for i, t := range jsRuntimes {
		if t.name == name {
			return t, i
		}
	}
	return nil, len(jsRuntimes)
}

func newestInstalledJsRuntime(target mirror.Platform) (*Binary, bool) {
	for _, t := range jsRuntimes {
		if binary, ok := newestInstalled(t, target); ok {
			return binary, true
		}
	}
	return nil, false
}

func prepareJsRuntime(target mirror.Platform) {
	if binary, ok := findSystemJsRuntime(); ok {
		jsRuntimeSlot.replace(binary)
		announceSystemJsRuntime(binary)
		return
	}

	binary, results := discover(jsRuntimes, target, installFunc(target))
	if binary == nil {
		binary, _ = newestInstalledJsRuntime(target)
	}
	jsRuntimeSlot.replace(binary)
	announceDownloadedJsRuntime(binary, results, target)
}

func announceSystemJsRuntime(binary *Binary) {
	t, _ := findJsRuntime(binary.Tool)
	if binary.Tool == jsRuntimes[0].name {
		logger.Infof("Using the system %s %s for yt-dlp", t.displayName, binary.Version)
		return
	}
	logger.Warnf("Using the system %s %s for yt-dlp; install Deno for the best YouTube support", t.displayName, binary.Version)
}

func announceDownloadedJsRuntime(binary *Binary, results []probeResult, target mirror.Platform) {
	if binary == nil {
		logger.Warnf("No JavaScript runtime is available for %s; install Deno with the system package manager or from https://deno.com, since YouTube playback may fail or miss formats without one", target)
		return
	}

	t, _ := findJsRuntime(binary.Tool)
	if binary.Tool == jsRuntimes[0].name {
		logger.Infof("Using the downloaded %s %s for yt-dlp", t.displayName, binary.Version)
		return
	}

	reason := fmt.Sprintf("%s has no build for %s", jsRuntimes[0].displayName, target)
	retry := ""
	if len(results) > 0 {
		reason = results[0].describe(target)
		if results[0].outcome != outcomeNoBuild {
			retry = fmt.Sprintf("; %s will be tried again on the next update check", jsRuntimes[0].displayName)
		}
	}
	logger.Warnf("%s, so the downloaded %s %s is used for yt-dlp instead%s", reason, t.displayName, binary.Version, retry)
}
