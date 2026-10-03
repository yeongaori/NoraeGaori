package locales

import "embed"

//go:embed en.json
var EnglishLocale []byte

//go:embed *.json
var Files embed.FS
