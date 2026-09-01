package settings

import "testing"

const checkToken = "abc123"

func TestComponentRoutingRecognisesEverySelect(t *testing.T) {
	checks := []struct {
		id     string
		action panelAction
		key    string
	}{
		{categoryPrefix + checkToken, actionSwitchCategory, ""},
		{pickPrefix + checkToken, actionPickSetting, ""},
		{customID(choicePrefix, "language", checkToken), actionChooseValue, "language"},
	}

	for _, check := range checks {
		action, key := routeComponent(check.id, checkToken)
		if action != check.action {
			t.Errorf("%q routed to action %d, want %d", check.id, action, check.action)
		}
		if key != check.key {
			t.Errorf("%q yielded key %q, want %q", check.id, key, check.key)
		}
	}
}

func TestComponentRoutingIgnoresAnotherPanelsToken(t *testing.T) {
	for _, id := range []string{
		categoryPrefix + "other-token",
		pickPrefix + "other-token",
		customID(choicePrefix, "language", "other-token"),
	} {
		if action, _ := routeComponent(id, checkToken); action != actionNone {
			t.Errorf("%q with a foreign token routed to action %d, want actionNone", id, action)
		}
	}
}

func TestComponentRoutingIgnoresUnrelatedCustomIDs(t *testing.T) {
	for _, id := range []string{"automix_pick_" + checkToken, "help_next", checkToken, ""} {
		if action, _ := routeComponent(id, checkToken); action != actionNone {
			t.Errorf("%q routed to action %d, want actionNone", id, action)
		}
	}
}

func TestModalRoutingRecognisesEveryEditableSetting(t *testing.T) {
	for _, spec := range settingSpecs {
		if spec.kind != settingText && spec.kind != settingNumber {
			continue
		}

		id := customID(modalPrefix, spec.key, checkToken)
		action, key := routeModal(id, checkToken)

		if action != actionSubmitModal {
			t.Errorf("%q routed to action %d, want actionSubmitModal", id, action)
		}
		if key != spec.key {
			t.Errorf("%q yielded key %q, want %q", id, key, spec.key)
		}
	}
}

func TestModalRoutingRejectsForeignTokensAndPrefixes(t *testing.T) {
	if action, _ := routeModal(customID(modalPrefix, "volume", "other-token"), checkToken); action != actionNone {
		t.Errorf("a foreign token routed to action %d, want actionNone", action)
	}
	if action, _ := routeModal(pickPrefix+checkToken, checkToken); action != actionNone {
		t.Errorf("a select id routed to action %d, want actionNone", action)
	}
}
