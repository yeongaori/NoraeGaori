//go:build testhooks

package dependency

import (
	"sync/atomic"

	"noraegaori/internal/dependency/mirror"
)

const HookArchiveName = archiveName
const HookLibDirectory = libDirectory
const HookOutcomeInstallFailed = outcomeInstallFailed
const HookOutcomeNoBuild = outcomeNoBuild
const HookOutcomeUnreachable = outcomeUnreachable
const HookPartialSuffix = partialSuffix

type HookAcceptFunc = acceptFunc
type HookOutcome = outcome
type HookSlot = slot
type HookTool = tool

type HookBinaryFields struct {
	Tool     string
	Path     string
	IsSystem bool
}

type HookToolFields struct {
	Name        string
	DisplayName string
	VersionFlag string
	Minimum     string
	Maximum     string
	Mirrors     []mirror.Mirror
}

var HookAddNvmNodeToPath = addNvmNodeToPath
var HookCompareVersions = compareVersions
var HookDiscover = discover
var HookEndProgress = &endProgress
var HookExtractMembers = extractMembers
var HookExtractWithProgress = extractWithProgress
var HookFfmpegSlot = &ffmpegSlot
var HookFfmpegTool = &ffmpegTool
var HookFindSystemFFmpeg = findSystemFFmpeg
var HookFindSystemJsRuntime = findSystemJsRuntime
var HookInstall = install
var HookInstallDirectory = installDirectory
var HookInstalledVersions = installedVersions
var HookIsFFmpegUpdateDue = isFFmpegUpdateDue
var HookJsRuntimeSlot = &jsRuntimeSlot
var HookJsRuntimes = &jsRuntimes
var HookNewestInstalled = newestInstalled
var HookPathFFmpeg = &pathFFmpeg
var HookPrepareFFmpeg = prepareFFmpeg
var HookPrepareJsRuntime = prepareJsRuntime
var HookReadVersion = readVersion
var HookRemoveDirectory = &removeDirectory
var HookRemoveRetired = removeRetired
var HookRetiredBinaries = &retiredBinaries
var HookRetiredMu = &retiredMu
var HookRunVersionCommand = runVersionCommand
var HookShowProgress = &showProgress
var HookUpdateJsRuntime = updateJsRuntime

func HookBuildBinary(fields HookBinaryFields) *Binary {
	return &Binary{Tool: fields.Tool, Path: fields.Path, isSystem: fields.IsSystem}
}

func HookBuildTool(fields HookToolFields) *tool {
	return &tool{
		name:        fields.Name,
		displayName: fields.DisplayName,
		versionFlag: fields.VersionFlag,
		minimum:     fields.Minimum,
		maximum:     fields.Maximum,
		mirrors:     fields.Mirrors,
	}
}

func (binary *Binary) HookIsSystem() *bool {
	return &binary.isSystem
}

func (result *probeResult) HookOutcome() *outcome {
	return &result.outcome
}

func (result *probeResult) HookTool() **tool {
	return &result.tool
}

func (result probeResult) HookDescribe(target mirror.Platform) string {
	return result.describe(target)
}

func (s *slot) HookActive() *atomic.Pointer[Binary] {
	return &s.active
}

func (s *slot) HookCurrent() *Binary {
	return s.current()
}

func (s *slot) HookReplace(next *Binary) {
	s.replace(next)
}

func (t *tool) HookDisplayName() *string {
	return &t.displayName
}

func (t *tool) HookMaximum() *string {
	return &t.maximum
}

func (t *tool) HookName() *string {
	return &t.name
}

func (t *tool) HookAccepts(version string) bool {
	return t.accepts(version)
}
