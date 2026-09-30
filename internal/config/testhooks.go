//go:build testhooks

package config

var HookAdminsConf = &adminsConf
var HookAdminsPath = &adminsPath
var HookConfig = &config
var HookConfigPath = &configPath
var HookIsDuplicateWriteEvent = isDuplicateWriteEvent
var HookLoadAdmins = loadAdmins
var HookLoadConfig = loadConfig
var HookNewFileWatcher = &newFileWatcher
var HookNotifyReloadCallbacks = notifyReloadCallbacks
var HookOnReloadCallbacks = &onReloadCallbacks
var HookOnReloadMux = &onReloadMux
var HookReloadWatchedFile = reloadWatchedFile
var HookSaveConfig = saveConfig
var HookWatchFiles = watchFiles
