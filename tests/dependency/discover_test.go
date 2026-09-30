package dependency_test

import (
	"errors"
	"strings"
	"testing"

	"noraegaori/internal/dependency"
	"noraegaori/internal/dependency/mirror"
)

func acceptAs(label string, failing ...string) (dependency.HookAcceptFunc, *[]string) {
	accepted := []string{}
	return func(t *dependency.HookTool, found *mirror.Candidate) (*dependency.Binary, error) {
		accepted = append(accepted, *t.HookName()+"@"+found.URL)
		for _, url := range failing {
			if found.URL == url {
				return nil, errors.New("checksum mismatch")
			}
		}
		return &dependency.Binary{Tool: *t.HookName(), Version: found.Version, Path: label + "/" + *t.HookName()}, nil
	}, &accepted
}

func foundAt(url, version string) *stubMirror {
	return &stubMirror{label: url, found: &mirror.Candidate{Version: version, URL: url}}
}

func TestDiscoverTakesTheFirstToolWithABuild(t *testing.T) {
	deno := stubTool("deno", foundAt("deno-github", "2.9.7"))
	node := stubTool("node", foundAt("nodejs", "22.23.3"))
	accept, accepted := acceptAs("lib")

	binary, results := dependency.HookDiscover([]*dependency.HookTool{deno, node}, linuxAmd64, accept)
	if binary == nil || binary.Tool != "deno" || len(results) != 0 {
		t.Fatalf("got %+v with %v, want deno and no failures", binary, results)
	}
	if len(*accepted) != 1 {
		t.Errorf("got %v, want node never installed", *accepted)
	}
}

func TestDiscoverFallsBackWhenDenoHasNoBuild(t *testing.T) {
	deno := stubTool("deno", noBuildMirror("deno-github"), noBuildMirror("deno-cdn"))
	node := stubTool("node", foundAt("nodejs", "22.23.3"))
	accept, _ := acceptAs("lib")

	binary, results := dependency.HookDiscover([]*dependency.HookTool{deno, node}, mirror.Platform{GOOS: "windows", GOARCH: "386"}, accept)
	if binary == nil || binary.Tool != "node" {
		t.Fatalf("got %+v, want node", binary)
	}
	if len(results) != 1 || *results[0].HookOutcome() != dependency.HookOutcomeNoBuild || *results[0].HookTool() != deno {
		t.Errorf("got %+v, want deno recorded as having no build", results)
	}
	if got := results[0].HookDescribe(mirror.Platform{GOOS: "windows", GOARCH: "386"}); got != "deno has no build for windows/386" {
		t.Errorf("got %q, want the platform named", got)
	}
}

func TestDiscoverTriesTheNextMirrorOfTheSameTool(t *testing.T) {
	github := unreachableMirror("deno-github")
	cdn := foundAt("deno-cdn", "2.9.7")
	node := foundAt("nodejs", "22.23.3")
	accept, accepted := acceptAs("lib")

	binary, _ := dependency.HookDiscover([]*dependency.HookTool{stubTool("deno", github, cdn), stubTool("node", node)}, linuxAmd64, accept)
	if binary == nil || binary.Tool != "deno" {
		t.Fatalf("got %+v, want deno from the second mirror", binary)
	}
	if node.calls.Load() != 0 || strings.Join(*accepted, ",") != "deno@deno-cdn" {
		t.Errorf("got accepted %v and %d node calls, want only the CDN build", *accepted, node.calls.Load())
	}
}

func TestDiscoverMovesToTheNextMirrorWhenAnInstallFails(t *testing.T) {
	accept, accepted := acceptAs("lib", "deno-github")

	binary, _ := dependency.HookDiscover([]*dependency.HookTool{stubTool("deno", foundAt("deno-github", "2.9.7"), foundAt("deno-cdn", "2.9.7"))}, linuxAmd64, accept)
	if binary == nil || binary.Tool != "deno" {
		t.Fatalf("got %+v, want deno from the CDN", binary)
	}
	if strings.Join(*accepted, ",") != "deno@deno-github,deno@deno-cdn" {
		t.Errorf("got %v, want both mirrors tried in order", *accepted)
	}
}

func TestDiscoverRecordsWhyEachToolFailed(t *testing.T) {
	deno := stubTool("deno", unreachableMirror("deno-github"), noBuildMirror("deno-cdn"))
	node := stubTool("node", foundAt("nodejs", "22.23.3"))
	bun := stubTool("bun", noBuildMirror("bun-github"))
	accept, _ := acceptAs("lib", "nodejs")

	binary, results := dependency.HookDiscover([]*dependency.HookTool{deno, node, bun}, linuxAmd64, accept)
	if binary != nil {
		t.Fatalf("got %+v, want nothing", binary)
	}

	want := []dependency.HookOutcome{dependency.HookOutcomeUnreachable, dependency.HookOutcomeInstallFailed, dependency.HookOutcomeNoBuild}
	if len(results) != len(want) {
		t.Fatalf("got %d results, want %d", len(results), len(want))
	}
	for i, result := range results {
		if *result.HookOutcome() != want[i] {
			t.Errorf("result %d: got outcome %d, want %d", i, *result.HookOutcome(), want[i])
		}
	}
	if !strings.Contains(results[0].HookDescribe(linuxAmd64), "deno mirrors are unreachable (deno-github is unreachable)") {
		t.Errorf("got %q, want the unreachable mirror described", results[0].HookDescribe(linuxAmd64))
	}
	if !strings.Contains(results[1].HookDescribe(linuxAmd64), "node failed to install (checksum mismatch)") {
		t.Errorf("got %q, want the failed install described", results[1].HookDescribe(linuxAmd64))
	}
}

func TestDiscoverKeepsAnInstallFailureOverALaterUnreachableMirror(t *testing.T) {
	accept, _ := acceptAs("lib", "deno-github")

	_, results := dependency.HookDiscover([]*dependency.HookTool{stubTool("deno", foundAt("deno-github", "2.9.7"), unreachableMirror("deno-cdn"))}, linuxAmd64, accept)
	if len(results) != 1 || *results[0].HookOutcome() != dependency.HookOutcomeInstallFailed {
		t.Errorf("got %+v, want the install failure kept", results)
	}
}

func TestDiscoverPassesTheToolBoundsToTheMirror(t *testing.T) {
	bun := stubTool("bun", foundAt("bun-github", "1.4.2"))
	*bun.HookMaximum() = "1.3.14"
	accept, _ := acceptAs("lib")

	binary, results := dependency.HookDiscover([]*dependency.HookTool{bun}, linuxAmd64, accept)
	if binary != nil || len(results) != 1 || *results[0].HookOutcome() != dependency.HookOutcomeNoBuild {
		t.Errorf("got %+v with %+v, want 1.4.2 rejected as no supported build", binary, results)
	}
}
