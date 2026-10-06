//go:build testhooks

package app

var HookConvertStoredStyles = convertStoredStyles
var HookForceExitAfter = forceExitAfter
var HookIsDisconnected = &isDisconnected
var HookIsShuttingDown = &isShuttingDown
var HookOnConnect = onConnect
var HookOnDisconnect = onDisconnect
var HookOnGuildDelete = onGuildDelete
var HookReconnectResumeDelay = &reconnectResumeDelay
var HookRedactToken = redactToken
var HookResumeAfterReconnect = &resumeAfterReconnect
var HookResumePlayersAfterReconnect = resumePlayersAfterReconnect
var HookResumeWaitingPlayers = &resumeWaitingPlayers
