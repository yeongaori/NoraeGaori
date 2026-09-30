//go:build testhooks

package logger

const HookClearLine = clearLine

type HookLockedWriter = lockedWriter

var HookConsole = &console
var HookConsoleIsTerminal = &consoleIsTerminal
var HookDebugMode = &debugMode
var HookDeriveTag = deriveTag
var HookEarlyBuf = &earlyBuf
var HookInfoBadge = &infoBadge
var HookIsDigits = isDigits
var HookIsGeneratedPart = isGeneratedPart
var HookIsTerminal = isTerminal
var HookLogFile = &logFile
var HookLogFilePath = &logFilePath
var HookOutMu = &outMu
var HookOutput = &output
var HookProgressDecade = &progressDecade
var HookProgressLine = &progressLine
