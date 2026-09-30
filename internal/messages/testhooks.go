//go:build testhooks

package messages

var HookActiveLocale = &activeLocale
var HookBuildLocale = buildLocale
var HookLocalesDir = localesDir
var HookReadLocaleFile = readLocaleFile

func (l *localeState) HookLang() *string {
	return &l.lang
}

func (l *localeState) HookLocale() **Locale {
	return &l.locale
}
