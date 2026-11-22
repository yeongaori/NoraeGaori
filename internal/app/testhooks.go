//go:build testhooks

package app

var HookConvertStoredStyles = convertStoredStyles
var HookForceExitAfter = forceExitAfter
var HookIsDisconnected = &isDisconnected
var HookIsShuttingDown = &isShuttingDown
var HookOnConnect = onConnect
var HookOnDisconnect = onDisconnect
var HookOnGuildDelete = onGuildDelete
var HookOnReady = onReady
var HookReadyOnce = &readyOnce
var HookReconnectResumeDelay = &reconnectResumeDelay
var HookRedactToken = redactToken
var HookRegisterDispatcher = &registerDispatcher
var HookResumeAfterReconnect = &resumeAfterReconnect
var HookResumePlayers = resumePlayers
var HookResumeWaitingPlayers = &resumeWaitingPlayers
var HookStartRPC = &startRPC
