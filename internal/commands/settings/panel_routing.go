package settings

import "strings"

type panelAction int

const (
	actionNone panelAction = iota
	actionSwitchCategory
	actionPickSetting
	actionChooseValue
	actionSubmitModal
)

func routeComponent(id, token string) (panelAction, string) {
	switch id {
	case categoryPrefix + token:
		return actionSwitchCategory, ""
	case pickPrefix + token:
		return actionPickSetting, ""
	}

	if strings.HasPrefix(id, choicePrefix) && strings.HasSuffix(id, "_"+token) {
		return actionChooseValue, settingKeyFrom(id, choicePrefix, token)
	}
	return actionNone, ""
}

func routeModal(id, token string) (panelAction, string) {
	if !strings.HasPrefix(id, modalPrefix) || !strings.HasSuffix(id, "_"+token) {
		return actionNone, ""
	}
	return actionSubmitModal, settingKeyFrom(id, modalPrefix, token)
}
