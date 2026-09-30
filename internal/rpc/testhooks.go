//go:build testhooks

package rpc

type HookStatusUpdater = statusUpdater
type HookUpdater = updater

type HookUpdaterFields struct {
	Session statusUpdater
	Cfg     *Config
}

var HookResolveActivityName = resolveActivityName
var HookRunning = &running
var HookRunningMu = &runningMu
var HookStopChan = &stopChan
var HookStopOnce = &stopOnce

func HookBuildUpdater(fields HookUpdaterFields) *updater {
	return &updater{session: fields.Session, cfg: fields.Cfg}
}

func (u *updater) HookCurrentIndex() *int {
	return &u.currentIndex
}

func (u *updater) HookIsFailing() *bool {
	return &u.isFailing
}

func (u *updater) HookUpdateActivity() {
	u.updateActivity()
}
