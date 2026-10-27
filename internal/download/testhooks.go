//go:build testhooks

package download

const HookUserAgent = userAgent

var HookDownloadStallTimeout = &downloadStallTimeout
var HookEndProgress = &endProgress
var HookReadRateLimit = readRateLimit
var HookShowProgress = &showProgress
