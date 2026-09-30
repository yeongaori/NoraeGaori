//go:build testhooks

package command

var HookAliases = &aliases
var HookBeforeCommand = &beforeCommand
var HookCanonicalCommandMap = canonicalCommandMap
var HookCanonicalContexts = canonicalContexts
var HookCommands = &commands
var HookDiffCommandSets = diffCommandSets
var HookFillMissingCommandDescriptions = fillMissingCommandDescriptions
var HookFocusedStringOption = focusedStringOption
var HookInteractionUserID = interactionUserID
var HookLookupAlias = lookupAlias
var HookLookupCommand = lookupCommand
