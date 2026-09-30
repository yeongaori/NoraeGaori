//go:build testhooks

package ytdlp

const HookCanaryActivated = canaryActivated
const HookCanaryPending = canaryPending
const HookCanaryRejected = canaryRejected
const HookCanaryRingSize = canaryRingSize
const HookCanaryTestCount = canaryTestCount
const HookChecksumAssetName = checksumAssetName
const HookChecksumSigAssetName = checksumSigAssetName
const HookGithubAPI = githubAPI
const HookGithubWeb = githubWeb
const HookNightlyRepo = nightlyRepo
const HookPythonAssetName = pythonAssetName
const HookRollbackThreshold = rollbackThreshold
const HookRollbackWindow = rollbackWindow
const HookStableRepo = stableRepo
const HookStableSuccessCount = stableSuccessCount
const HookStalePendingTimeout = stalePendingTimeout
const HookVersionDataFile = versionDataFile

type HookChannelOutcome = channelOutcome
type HookPersistedState = persistedState

type HookChannelOutcomeFields struct {
	Updated      bool
	CanaryFailed bool
}

type HookVersionManagerFields struct {
	State persistedState
}

var HookActiveVersionIsHealthy = activeVersionIsHealthy
var HookConfiguredChannelFn = &configuredChannelFn
var HookEnsureVersionBinary = ensureVersionBinary
var HookFixedCanaryIDs = &fixedCanaryIDs
var HookGetLatestReleaseFn = &getLatestReleaseFn
var HookGetReleasesFn = &getReleasesFn
var HookInstallFallbackVersion = installFallbackVersion
var HookInstallVersionBinary = installVersionBinary
var HookLatestReleaseRepo = latestReleaseRepo
var HookLookPath = &lookPath
var HookPickAsset = pickAsset
var HookProbeStreamReachable = probeStreamReachable
var HookReleasesURL = releasesURL
var HookResolveCurrentVersion = resolveCurrentVersion
var HookRunCanary = &runCanary
var HookRunCanaryAndActivate = runCanaryAndActivate
var HookSigningKeyArmor = &signingKeyArmor
var HookUpdateChannelFn = &updateChannelFn
var HookUpdateFromChannel = updateFromChannel
var HookVersionMgr = &versionMgr
var HookYtdlpSigningKey = &ytdlpSigningKey

func HookBuildChannelOutcome(fields HookChannelOutcomeFields) *channelOutcome {
	return &channelOutcome{updated: fields.Updated, canaryFailed: fields.CanaryFailed}
}

func HookBuildVersionManager(fields HookVersionManagerFields) *VersionManager {
	return &VersionManager{state: fields.State}
}

func (c *canaryResult) HookInconclusive() *bool {
	return &c.inconclusive
}

func (c *channelOutcome) HookCanaryFailed() *bool {
	return &c.canaryFailed
}

func (c *channelOutcome) HookUpdated() *bool {
	return &c.updated
}

func (versionmanager *VersionManager) HookState() *persistedState {
	return &versionmanager.state
}

func (versionmanager *VersionManager) HookAddToCanaryRing(videoID string) {
	versionmanager.addToCanaryRing(videoID)
}

func (versionmanager *VersionManager) HookCleanupOldVersions() {
	versionmanager.cleanupOldVersions()
}

func (versionmanager *VersionManager) HookGetCanaryIDs() []string {
	return versionmanager.getCanaryIDs()
}

func (versionmanager *VersionManager) HookLoad() error {
	return versionmanager.load()
}

func (versionmanager *VersionManager) HookSelectBestVersion() string {
	return versionmanager.selectBestVersion()
}

func (versionmanager *VersionManager) HookShouldRollback() bool {
	return versionmanager.shouldRollback()
}

func (versionmanager *VersionManager) HookTestExtraction(binaryPath string, videoID string) canaryResult {
	return versionmanager.testExtraction(binaryPath, videoID)
}

func (versionmanager *VersionManager) HookTryPromoteVerified() {
	versionmanager.tryPromoteVerified()
}
