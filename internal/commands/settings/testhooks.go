//go:build testhooks

package settings

const HookCategoryGeneral = categoryGeneral
const HookCategoryMixing = categoryMixing
const HookCategoryPlayback = categoryPlayback
const HookCategoryRoute = categoryRoute
const HookDefaultChoiceValue = defaultChoiceValue
const HookModalRoute = modalRoute
const HookModalValueID = modalValueID
const HookPickRoute = pickRoute
const HookSelectOptionLimit = selectOptionLimit
const HookSettingChoice = settingChoice
const HookSettingNumber = settingNumber
const HookSettingText = settingText
const HookSettingToggle = settingToggle
const HookValueOff = valueOff
const HookValueOn = valueOn
const HookValueRepeatAll = valueRepeatAll
const HookValueRepeatOff = valueRepeatOff
const HookValueRepeatSingle = valueRepeatSingle

type HookPanelTarget = panelTarget
type HookSettingArguments = settingArguments
type HookSettingSpec = settingSpec

type HookPanelTargetFields struct {
	IsAdmin  bool
	Category string
	Key      string
}

type HookSettingArgumentsFields struct {
	Value       string
	HasValue    bool
	Number      *settingSpec
	NumberValue string
}

type HookSettingSpecFields struct {
	Key        string
	Category   string
	Kind       settingKind
	AdminOnly  bool
	HasDefault bool
	Options    func() []string
	Read       func(guildID string) (string, error)
}

var HookAllowedToEdit = allowedToEdit
var HookApplySetting = applySetting
var HookApplySettingArguments = applySettingArguments
var HookBuildSettingMenu = buildSettingMenu
var HookBuildSettingModal = buildSettingModal
var HookBuildSettingsComponents = buildSettingsComponents
var HookBuildSettingsEmbed = buildSettingsEmbed
var HookCanEditAdminSettings = canEditAdminSettings
var HookCategoryLabel = categoryLabel
var HookChoiceOptions = choiceOptions
var HookCurrentValue = currentValue
var HookDefaultCategory = defaultCategory
var HookDefaultFor = defaultFor
var HookErrNotInteger = &errNotInteger
var HookErrNotNumber = &errNotNumber
var HookErrOutOfRange = &errOutOfRange
var HookErrTooLong = &errTooLong
var HookErrUnknownValue = &errUnknownValue
var HookFindModalValue = findModalValue
var HookFindSetting = findSetting
var HookFormatFloat = formatFloat
var HookFormatSettingValue = formatSettingValue
var HookHasSettingMenu = hasSettingMenu
var HookHasVisibleSetting = hasVisibleSetting
var HookIsKnownCategory = isKnownCategory
var HookNewPanelView = newPanelView
var HookNextValue = nextValue
var HookNormalizeValue = normalizeValue
var HookPanelStrings = panelStrings
var HookParsePanelArguments = parsePanelArguments
var HookParseSettingArguments = parseSettingArguments
var HookRegisterPanelRoutes = registerPanelRoutes
var HookRegisterSettingMenus = registerSettingMenus
var HookRenderPanel = renderPanel
var HookRepeatValues = &repeatValues
var HookRequestedCategory = requestedCategory
var HookSettingCategories = &settingCategories
var HookSettingLabel = settingLabel
var HookSettingMenuValues = settingMenuValues
var HookSettingRow = settingRow
var HookSettingSpecs = &settingSpecs
var HookSettingsInCategory = settingsInCategory
var HookValidationMessage = validationMessage
var HookVisibleCategories = visibleCategories
var HookWriteNormalization = writeNormalization

func HookBuildPanelTarget(fields HookPanelTargetFields) *panelTarget {
	return &panelTarget{isAdmin: fields.IsAdmin, category: fields.Category, key: fields.Key}
}

func HookBuildSettingArguments(fields HookSettingArgumentsFields) *settingArguments {
	return &settingArguments{
		value:       fields.Value,
		hasValue:    fields.HasValue,
		number:      fields.Number,
		numberValue: fields.NumberValue,
	}
}

func HookBuildSettingSpec(fields HookSettingSpecFields) *settingSpec {
	return &settingSpec{
		key:        fields.Key,
		category:   fields.Category,
		kind:       fields.Kind,
		adminOnly:  fields.AdminOnly,
		hasDefault: fields.HasDefault,
		options:    fields.Options,
		read:       fields.Read,
	}
}

func (view *panelView) HookCategory() *string {
	return &view.category
}

func (view *panelView) HookDisplayValue(spec *settingSpec) string {
	return view.displayValue(spec)
}

func (s *settingArguments) HookHasValue() *bool {
	return &s.hasValue
}

func (s *settingArguments) HookNumber() **settingSpec {
	return &s.number
}

func (s *settingArguments) HookNumberValue() *string {
	return &s.numberValue
}

func (s *settingArguments) HookValue() *string {
	return &s.value
}

func (spec *settingSpec) HookAdminOnly() *bool {
	return &spec.adminOnly
}

func (spec *settingSpec) HookCategory() *string {
	return &spec.category
}

func (spec *settingSpec) HookKey() *string {
	return &spec.key
}

func (spec *settingSpec) HookKind() *settingKind {
	return &spec.kind
}

func (spec *settingSpec) HookMax() *float64 {
	return &spec.max
}

func (spec *settingSpec) HookMin() *float64 {
	return &spec.min
}
