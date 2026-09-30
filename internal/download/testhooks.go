//go:build testhooks

package download

const HookUserAgent = userAgent

var HookDownloadStallTimeout = &downloadStallTimeout
var HookEndProgress = &endProgress
var HookRateLimitBytesPerSecond = rateLimitBytesPerSecond
var HookShowProgress = &showProgress
