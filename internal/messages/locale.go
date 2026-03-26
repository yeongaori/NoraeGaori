package messages

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"

	"noraegaori/locales"
)

// Locale represents a complete set of translated strings
type Locale struct {
	Errors       ErrorMessages              `json:"errors"`
	Titles       TitleMessages              `json:"titles"`
	Fields       FieldMessages              `json:"fields"`
	Descriptions DescriptionMessages        `json:"descriptions"`
	Footers      FooterMessages             `json:"footers"`
	Buttons      ButtonMessages             `json:"buttons"`
	SelectMenus  SelectMenuMessages         `json:"select_menus"`
	Votes        VoteMessages               `json:"votes"`
	Help         HelpMessages               `json:"help"`
	Commands     map[string]CommandStrings   `json:"commands"`
	Voice        VoiceMessages              `json:"voice"`
	Admin        AdminMessages              `json:"admin"`
	Settings     SettingsMessages           `json:"settings"`
	Status       StatusMessages             `json:"status"`
	Music        MusicMessages              `json:"music"`
	Queue        QueueMessages              `json:"queue"`
	Player       PlayerMessages             `json:"player"`
	YouTube      YouTubeMessages            `json:"youtube"`
	VoiceHandler VoiceHandlerMessages       `json:"voice_handler"`
	RPC          RPCMessages                `json:"rpc"`
}

type ErrorMessages struct {
	NotInVoiceChannel     string `json:"not_in_voice_channel"`
	EmptyQueue            string `json:"empty_queue"`
	SongNotFound          string `json:"song_not_found"`
	PermissionDenied      string `json:"permission_denied"`
	AdminOnly             string `json:"admin_only"`
	AlreadyVoted          string `json:"already_voted"`
	DuplicateSong         string `json:"duplicate_song"`
	UnknownCommand        string `json:"unknown_command"`
	CommandExecutionError string `json:"command_execution_error"`
	MustBeInBotChannel    string `json:"must_be_in_bot_channel"`
}

type TitleMessages struct {
	Added        string `json:"added"`
	Success      string `json:"success"`
	Removed      string `json:"removed"`
	Skipped      string `json:"skipped"`
	Resumed      string `json:"resumed"`
	Paused       string `json:"paused"`
	RepeatAll    string `json:"repeat_all"`
	RepeatSingle string `json:"repeat_single"`
	RepeatOff    string `json:"repeat_off"`
	Searching    string `json:"searching"`
	Loading      string `json:"loading"`
	NowPlaying   string `json:"now_playing"`
	Queue        string `json:"queue"`
	Help         string `json:"help"`
	SearchResults   string `json:"search_results"`
	PlaylistFound   string `json:"playlist_found"`
	PlaylistAdded   string `json:"playlist_added"`
	PlaylistStart   string `json:"playlist_start"`
	SkipVote        string `json:"skip_vote"`
	StopVote        string `json:"stop_vote"`
	SystemInfo      string `json:"system_info"`
	Warning         string `json:"warning"`
	Duplicate       string `json:"duplicate"`
	Unavailable     string `json:"unavailable"`
	Error           string `json:"error"`
	EmptyQueue      string `json:"empty_queue"`
	NoSong          string `json:"no_song"`
	NoPermission    string `json:"no_permission"`
	AlreadyVoted    string `json:"already_voted"`
}

type FieldMessages struct {
	Uploader       string `json:"uploader"`
	Duration       string `json:"duration"`
	Requester      string `json:"requester"`
	NextSong       string `json:"next_song"`
	TotalSongs     string `json:"total_songs"`
	CurrentVote    string `json:"current_vote"`
	RequiredVote   string `json:"required_vote"`
	VoteResult     string `json:"vote_result"`
	RemovedSongs   string `json:"removed_songs"`
	CurrentPrefix  string `json:"current_prefix"`
	TotalCommands  string `json:"total_commands"`
	CPUInfo        string `json:"cpu_info"`
	CPUUsage       string `json:"cpu_usage"`
	TotalMemory    string `json:"total_memory"`
	MemoryUsage    string `json:"memory_usage"`
	BotMemory      string `json:"bot_memory"`
	ServerMemory   string `json:"server_memory"`
	PlayingServers string `json:"playing_servers"`
}

type DescriptionMessages struct {
	Searching        string `json:"searching"`
	Loading          string `json:"loading"`
	PlaylistConfirm  string `json:"playlist_confirm"`
	PlaylistAdding   string `json:"playlist_adding"`
	PlaylistSkipped  string `json:"playlist_skipped"`
	VoteAdded        string `json:"vote_added"`
	Skipped          string `json:"skipped"`
	Paused           string `json:"paused"`
	Resumed          string `json:"resumed"`
	RepeatAll        string `json:"repeat_all"`
	RepeatSingle     string `json:"repeat_single"`
	RepeatOff        string `json:"repeat_off"`
	RepeatOffRemoved string `json:"repeat_off_removed"`
	SongsRemoved     string `json:"songs_removed"`
	EmptyQueue       string `json:"empty_queue"`
	SearchPrompt     string `json:"search_prompt"`
	SystemStatus     string `json:"system_status"`
}

type FooterMessages struct {
	Pagination      string `json:"pagination"`
	HelpPagination  string `json:"help_pagination"`
	PlaylistConfirm string `json:"playlist_confirm"`
	RequestedBy     string `json:"requested_by"`
	VoteReaction    string `json:"vote_reaction"`
}

type ButtonMessages struct {
	Previous string `json:"previous"`
	Next     string `json:"next"`
}

type SelectMenuMessages struct {
	Placeholder string `json:"placeholder"`
}

type VoteMessages struct {
	More             string `json:"more"`
	Expired          string `json:"expired"`
	StopAlreadyVoted string `json:"stop_already_voted"`
}

type HelpMessages struct {
	CommandFormat     string `json:"command_format"`
	AdminMarker       string `json:"admin_marker"`
	NoCommandsTitle   string `json:"no_commands_title"`
	NoCommandsDesc    string `json:"no_commands_desc"`
	MessageLabel      string `json:"message_label"`
	AliasLabel        string `json:"alias_label"`
	SlashLabel        string `json:"slash_label"`
	ExampleLabel      string `json:"example_label"`
	TotalCommandsValue string `json:"total_commands_value"`
}

type CommandStrings struct {
	Description string            `json:"description"`
	Options     map[string]string `json:"options"`
	Usage       string            `json:"usage"`
	Example     string            `json:"example"`
	Aliases     []string          `json:"aliases"`
}

type VoiceMessages struct {
	EnterVoiceChannel    string `json:"enter_voice_channel"`
	EnterVoiceOrSpecify  string `json:"enter_voice_or_specify"`
	JoinFailedTitle      string `json:"join_failed_title"`
	JoinFailedDesc       string `json:"join_failed_desc"`
	JoinSuccessTitle     string `json:"join_success_title"`
	JoinSuccessDesc      string `json:"join_success_desc"`
	JoinSuccessChannel   string `json:"join_success_channel"`
	LeaveFailedTitle     string `json:"leave_failed_title"`
	LeaveFailedDesc      string `json:"leave_failed_desc"`
	LeaveSuccessTitle    string `json:"leave_success_title"`
	LeaveSuccessDesc     string `json:"leave_success_desc"`
	SwitchFailedTitle    string `json:"switch_failed_title"`
	SwitchFailedChannel  string `json:"switch_failed_channel"`
	SwitchFailedQueue    string `json:"switch_failed_queue"`
	SwitchSuccessTitle   string `json:"switch_success_title"`
	SwitchSuccessDesc    string `json:"switch_success_desc"`
	SwitchSuccessChannel string `json:"switch_success_channel"`
}

type AdminMessages struct {
	MentionTarget      string `json:"mention_target"`
	NoSongsToDelete    string `json:"no_songs_to_delete"`
	InvalidMention     string `json:"invalid_mention"`
	UserNotFound       string `json:"user_not_found"`
	UserNoSongs        string `json:"user_no_songs"`
	ExcludingCurrent   string `json:"excluding_current"`
	RemoveFailed       string `json:"remove_failed"`
	DeleteCompleteTitle string `json:"delete_complete_title"`
	DeleteCompleteDesc  string `json:"delete_complete_desc"`
	EnterPositions     string `json:"enter_positions"`
	EnterValidRange    string `json:"enter_valid_range"`
	SamePosition       string `json:"same_position"`
	CannotMovePlaying  string `json:"cannot_move_playing"`
	MoveFailed         string `json:"move_failed"`
	MoveCompleteTitle  string `json:"move_complete_title"`
	MoveCompleteDesc   string `json:"move_complete_desc"`
	NoSongsTitle       string `json:"no_songs_title"`
	NoSongsDesc        string `json:"no_songs_desc"`
	StopFailed         string `json:"stop_failed"`
	ForceStopTitle     string `json:"force_stop_title"`
	ForceStopDesc      string `json:"force_stop_desc"`
}

type SettingsMessages struct {
	StatusOn              string `json:"status_on"`
	StatusOff             string `json:"status_off"`
	SponsorBlockError     string `json:"sponsorblock_error"`
	SponsorBlockTitle     string `json:"sponsorblock_title"`
	SponsorBlockDesc      string `json:"sponsorblock_desc"`
	SponsorBlockWhatTitle string `json:"sponsorblock_what_title"`
	SponsorBlockWhatDesc  string `json:"sponsorblock_what_desc"`
	NoteTitle             string `json:"note_title"`
	SettingApplyNext      string `json:"setting_apply_next"`
	ShowTrackError        string `json:"showtrack_error"`
	ShowTrackTitle        string `json:"showtrack_title"`
	ShowTrackDesc         string `json:"showtrack_desc"`
	ShowTrackWhatTitle    string `json:"showtrack_what_title"`
	ShowTrackWhatDesc     string `json:"showtrack_what_desc"`
	NormalizationError    string `json:"normalization_error"`
	NormalizationTitle    string `json:"normalization_title"`
	NormalizationDesc     string `json:"normalization_desc"`
	NormalizationWhatTitle string `json:"normalization_what_title"`
	NormalizationWhatDesc  string `json:"normalization_what_desc"`
	CurrentPrefixTitle    string `json:"current_prefix_title"`
	CurrentPrefixDesc     string `json:"current_prefix_desc"`
	PrefixEmpty           string `json:"prefix_empty"`
	PrefixTooLong         string `json:"prefix_too_long"`
	PrefixError           string `json:"prefix_error"`
	PrefixChangedTitle    string `json:"prefix_changed_title"`
	PrefixChangedDesc     string `json:"prefix_changed_desc"`
	PrefixExampleTitle    string `json:"prefix_example_title"`
	PrefixExampleValue    string `json:"prefix_example_value"`
	PrefixSlashNote       string `json:"prefix_slash_note"`
}

type StatusMessages struct {
	LoadingTitle      string `json:"loading_title"`
	LoadingDesc       string `json:"loading_desc"`
	Title             string `json:"title"`
	Description       string `json:"description"`
	CPUInfoValue      string `json:"cpu_info_value"`
	CPUUsageValue     string `json:"cpu_usage_value"`
	TotalMemoryValue  string `json:"total_memory_value"`
	MemoryUsageValue  string `json:"memory_usage_value"`
	BotMemoryValue    string `json:"bot_memory_value"`
	ServerMemoryValue string `json:"server_memory_value"`
	PlayingServersValue string `json:"playing_servers_value"`
}

type MusicMessages struct {
	EnterQuery            string `json:"enter_query"`
	QueueCreateFailed     string `json:"queue_create_failed"`
	SongAddFailed         string `json:"song_add_failed"`
	AddedAsNext           string `json:"added_as_next"`
	PlaylistInfoFailed    string `json:"playlist_info_failed"`
	PlaylistConfirmDesc   string `json:"playlist_confirm_desc"`
	PlaylistConfirmFooter string `json:"playlist_confirm_footer"`
	VideoUnavailableTitle string `json:"video_unavailable_title"`
	VideoUnavailableDesc  string `json:"video_unavailable_desc"`
	VideoUnavailableFooter string `json:"video_unavailable_footer"`
	VideoWithPlaylistDuplicate string `json:"video_with_playlist_duplicate"`
	VideoWithPlaylistFound     string `json:"video_with_playlist_found"`
	VideoWithPlaylistFooter    string `json:"video_with_playlist_footer"`
	NotPlayingOrLoading   string `json:"not_playing_or_loading"`
	PauseFailed           string `json:"pause_failed"`
	NoSongsToResume       string `json:"no_songs_to_resume"`
	AlreadyPlaying        string `json:"already_playing"`
	PlaybackStartError    string `json:"playback_start_error"`
	QueueNotFound         string `json:"queue_not_found"`
	LiveCheckingTitle     string `json:"live_checking_title"`
	LiveCheckingDesc      string `json:"live_checking_desc"`
	LiveEndedTitle        string `json:"live_ended_title"`
	LiveEndedNoQueue      string `json:"live_ended_no_queue"`
	LiveEndedSkip         string `json:"live_ended_skip"`
	LiveStartTitle        string `json:"live_start_title"`
	LiveStartDesc         string `json:"live_start_desc"`
	ResumeStartTitle      string `json:"resume_start_title"`
	ResumeStartDesc       string `json:"resume_start_desc"`
	EnterVoiceChannel     string `json:"enter_voice_channel"`
	ServerInfoFailed      string `json:"server_info_failed"`
	SkipFailedTitle       string `json:"skip_failed_title"`
	SkipFailedDesc        string `json:"skip_failed_desc"`
	PlaybackEndedTitle    string `json:"playback_ended_title"`
	PlaybackEndedSkip     string `json:"playback_ended_skip"`
	ForceSkipped          string `json:"force_skipped"`
	ForceSkippedEnded     string `json:"force_skipped_ended"`
	StopFailedTitle       string `json:"stop_failed_title"`
	StopFailedDesc        string `json:"stop_failed_desc"`
	StopSuccessTitle      string `json:"stop_success_title"`
	StopSuccessDesc       string `json:"stop_success_desc"`
	StopAlreadyVoted      string `json:"stop_already_voted"`
	NowPlayingLoading     string `json:"nowplaying_loading"`
	NowPlayingPlaying     string `json:"nowplaying_playing"`
	NowPlayingPaused      string `json:"nowplaying_paused"`
	VolumeQueryFailed     string `json:"volume_query_failed"`
	CurrentVolumeTitle    string `json:"current_volume_title"`
	CurrentVolumeDesc     string `json:"current_volume_desc"`
	VolumeNotNumber       string `json:"volume_not_number"`
	VolumeOutOfRange      string `json:"volume_out_of_range"`
	VolumeSetFailed       string `json:"volume_set_failed"`
	VolumeSetTitle        string `json:"volume_set_title"`
	VolumeSetDesc         string `json:"volume_set_desc"`
	RepeatSetFailed       string `json:"repeat_set_failed"`
	PlaylistAddingTitle   string `json:"playlist_adding_title"`
	PlaylistAddingAll     string `json:"playlist_adding_all"`
	PlaylistAddingRest    string `json:"playlist_adding_rest"`
	PlaylistTimeoutTitle  string `json:"playlist_timeout_title"`
	PlaylistTimeoutDesc   string `json:"playlist_timeout_desc"`
	PlaylistCompleteDesc  string `json:"playlist_complete_desc"`
	PlaylistSkippedCount  string `json:"playlist_skipped_count"`
	PlaylistSkippedOrDup  string `json:"playlist_skipped_or_dup"`
	PlaylistAddedCount    string `json:"playlist_added_count"`
	PlaylistAddedSongs    string `json:"playlist_added_songs"`
	PlaylistSongsUnit     string `json:"playlist_songs_unit"`
	ErrorPrivateVideo     string `json:"error_private_video"`
	ErrorDeletedVideo     string `json:"error_deleted_video"`
	ErrorAgeRestricted    string `json:"error_age_restricted"`
	ErrorGeoRestricted    string `json:"error_geo_restricted"`
	ErrorMembersOnly      string `json:"error_members_only"`
	ErrorPremiumOnly      string `json:"error_premium_only"`
	ErrorCopyright        string `json:"error_copyright"`
	ErrorBlocked          string `json:"error_blocked"`
	ErrorUnavailable      string `json:"error_unavailable"`
}

type QueueMessages struct {
	LiveBadge            string `json:"live_badge"`
	EnterPosition        string `json:"enter_position"`
	NoUserSongs          string `json:"no_user_songs"`
	OnlyCurrentSong      string `json:"only_current_song"`
	NoSongsToRemoveTitle string `json:"no_songs_to_remove_title"`
	RemoveFailed         string `json:"remove_failed"`
	SongsRemovedTitle    string `json:"songs_removed_title"`
	SongsRemovedAll      string `json:"songs_removed_all"`
	InvalidRange         string `json:"invalid_range"`
	RangeIncludesCurrent string `json:"range_includes_current"`
	NoUserSongsInRange   string `json:"no_user_songs_in_range"`
	RangeRemoved         string `json:"range_removed"`
	EnterValidRange      string `json:"enter_valid_range"`
	CannotRemoveCurrent  string `json:"cannot_remove_current"`
	OnlyOwnSongs         string `json:"only_own_songs"`
	SongRemoved          string `json:"song_removed"`
	EnterSearchQuery     string `json:"enter_search_query"`
	SearchingTitle       string `json:"searching_title"`
	SearchingDesc        string `json:"searching_desc"`
	NoResultsTitle       string `json:"no_results_title"`
	NoResultsDesc        string `json:"no_results_desc"`
	SearchResultsTitle   string `json:"search_results_title"`
	SearchResultsDesc    string `json:"search_results_desc"`
	SelectPlaceholder    string `json:"select_placeholder"`
	OnlyRequester        string `json:"only_requester"`
	AlreadySelectedTitle string `json:"already_selected_title"`
	AlreadySelectedDesc  string `json:"already_selected_desc"`
	ProcessingTitle      string `json:"processing_title"`
	ProcessingDesc       string `json:"processing_desc"`
	SearchAddError       string `json:"search_add_error"`
	SearchTimeoutTitle   string `json:"search_timeout_title"`
	SearchTimeoutDesc    string `json:"search_timeout_desc"`
	SkipToEnterPosition  string `json:"skipto_enter_position"`
	SkipToCurrent        string `json:"skipto_current"`
	SkipToFailed         string `json:"skipto_failed"`
	SkipToCompleteTitle  string `json:"skipto_complete_title"`
	SkipToCompleteDesc   string `json:"skipto_complete_desc"`
	SkipToSkippedSongs   string `json:"skipto_skipped_songs"`
	SkipToSongsCount     string `json:"skipto_songs_count"`
	SwapEnterPositions   string `json:"swap_enter_positions"`
	CannotSwapCurrent    string `json:"cannot_swap_current"`
	OnlyOwnSwap          string `json:"only_own_swap"`
	SwapFailed           string `json:"swap_failed"`
	SwapCompleteTitle    string `json:"swap_complete_title"`
	SwapCompleteDesc     string `json:"swap_complete_desc"`
	QueueNextButton      string `json:"queue_next_button"`
}

type PlayerMessages struct {
	PlaybackStarted          string `json:"playback_started"`
	NowPlaying               string `json:"now_playing"`
	StreamReconnectedTitle   string `json:"stream_reconnected_title"`
	StreamReconnectedDesc    string `json:"stream_reconnected_desc"`
	StreamReconnectingTitle  string `json:"stream_reconnecting_title"`
	StreamReconnectingDesc   string `json:"stream_reconnecting_desc"`
	PlaybackFailedTitle      string `json:"playback_failed_title"`
	StreamReconnectFailedTitle string `json:"stream_reconnect_failed_title"`
	StreamReconnectFailedDesc  string `json:"stream_reconnect_failed_desc"`
	MaxRetriesSkipping       string `json:"max_retries_skipping"`
	LeavingEmptyDesc         string `json:"leaving_empty_desc"`
	LeavingEmptyFooter       string `json:"leaving_empty_footer"`
	LeavingErrorDesc         string `json:"leaving_error_desc"`
	LeavingErrorFooter       string `json:"leaving_error_footer"`
	LeavingDefaultDesc       string `json:"leaving_default_desc"`
	ErrorPrivateVideo        string `json:"error_private_video"`
	ErrorDeletedVideo        string `json:"error_deleted_video"`
	ErrorAgeRestricted       string `json:"error_age_restricted"`
	ErrorGeoRestricted       string `json:"error_geo_restricted"`
	ErrorMembersOnly         string `json:"error_members_only"`
	ErrorPremiumOnly         string `json:"error_premium_only"`
	ErrorCopyright           string `json:"error_copyright"`
	ErrorBlocked             string `json:"error_blocked"`
	ErrorRemovedByUploader   string `json:"error_removed_by_uploader"`
	ErrorAccountTerminated   string `json:"error_account_terminated"`
	ErrorUnavailable         string `json:"error_unavailable"`
}

type YouTubeMessages struct {
	ErrorPrivateVideo     string `json:"error_private_video"`
	ErrorAgeRestricted    string `json:"error_age_restricted"`
	ErrorGeoRestricted    string `json:"error_geo_restricted"`
	ErrorMembersOnly      string `json:"error_members_only"`
	ErrorPremiumOnly      string `json:"error_premium_only"`
	ErrorCopyright        string `json:"error_copyright"`
	ErrorUnplayable       string `json:"error_unplayable"`
	ErrorUnplayableReason string `json:"error_unplayable_reason"`
	ErrorDeletedVideo     string `json:"error_deleted_video"`
	ErrorUnavailable      string `json:"error_unavailable"`
	ErrorUnavailableReason string `json:"error_unavailable_reason"`
	ErrorContentCheck     string `json:"error_content_check"`
	ErrorAgeVerification  string `json:"error_age_verification"`
	ErrorRegionRestricted string `json:"error_region_restricted"`
	ErrorPrivateOrDeleted string `json:"error_private_or_deleted"`
}

type VoiceHandlerMessages struct {
	AutoPauseTitle string `json:"auto_pause_title"`
	AutoPauseDesc  string `json:"auto_pause_desc"`
}

type RPCMessages struct {
	ActivityMusic      string `json:"activity_music"`
	ActivitySong       string `json:"activity_song"`
	ActivityPlaylist   string `json:"activity_playlist"`
	ActivityMusicVideo string `json:"activity_music_video"`
}

// currentLocale holds the loaded locale (initialized to empty struct to avoid nil panics)
var currentLocale = &Locale{}

// currentLang holds the language code of the loaded locale (e.g. "en", "ko")
var currentLang = "en"

// T returns the current locale. Returns nil if no locale has been loaded.
func T() *Locale {
	return currentLocale
}

// Lang returns the current language code (e.g. "en", "ko").
func Lang() string {
	return currentLang
}

func LoadLocale(lang string) error {
	currentLang = lang
	localesDir := "locales"

	enData, err := readLocaleFile(filepath.Join(localesDir, "en.json"))
	if err != nil {
		enData = locales.EnglishLocale
	}

	var base Locale
	if err := json.Unmarshal(enData, &base); err != nil {
		return fmt.Errorf("failed to parse English fallback locale: %w", err)
	}

	if lang == "en" {
		currentLocale = &base
		applyLocale(&base)
		return nil
	}

	// Try to load the requested language and overlay on top of English
	langData, err := readLocaleFile(filepath.Join(localesDir, lang+".json"))
	if err != nil {
		// Locale file not found — fall back to English
		currentLocale = &base
		applyLocale(&base)
		return fmt.Errorf("locale %q not found, falling back to English: %w", lang, err)
	}

	var overlay Locale
	if err := json.Unmarshal(langData, &overlay); err != nil {
		// Invalid JSON — fall back to English
		currentLocale = &base
		applyLocale(&base)
		return fmt.Errorf("locale %q has invalid JSON, falling back to English: %w", lang, err)
	}

	// Merge: overlay non-zero values onto the English base
	mergeLocale(&base, &overlay)

	currentLocale = &base
	applyLocale(&base)
	return nil
}

// readLocaleFile reads a locale JSON file, trying the given path first,
// then a path relative to the source file location.
func readLocaleFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return data, nil
	}
	// Try relative to the source file location
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	altPath := filepath.Join(dir, "..", "..", path)
	return os.ReadFile(altPath)
}

// mergeLocale copies non-zero fields from overlay into base.
// For string fields, empty strings in overlay are not copied (base keeps its value).
// For map fields, individual entries are merged.
// For the Commands map (map[string]CommandStrings), each command's fields
// are merged individually so partial translations still work.
func mergeLocale(base, overlay *Locale) {
	mergeStruct(reflect.ValueOf(base).Elem(), reflect.ValueOf(overlay).Elem())
}

// mergeStruct recursively merges non-zero overlay fields into base.
func mergeStruct(base, overlay reflect.Value) {
	for i := 0; i < base.NumField(); i++ {
		baseField := base.Field(i)
		overlayField := overlay.Field(i)

		switch baseField.Kind() {
		case reflect.String:
			if overlayField.String() != "" {
				baseField.SetString(overlayField.String())
			}
		case reflect.Struct:
			mergeStruct(baseField, overlayField)
		case reflect.Map:
			mergeMap(baseField, overlayField)
		case reflect.Slice:
			if overlayField.Len() > 0 {
				baseField.Set(overlayField)
			}
		}
	}
}

// mergeMap merges overlay map entries into base map.
// For struct-valued maps, fields are merged individually.
func mergeMap(base, overlay reflect.Value) {
	if overlay.IsNil() {
		return
	}
	if base.IsNil() {
		base.Set(overlay)
		return
	}
	for _, key := range overlay.MapKeys() {
		overlayVal := overlay.MapIndex(key)
		baseVal := base.MapIndex(key)

		// Check if the map value type is a struct (e.g., CommandStrings)
		mapElemType := base.Type().Elem()
		if mapElemType.Kind() == reflect.Struct {
			// Create an addressable copy to merge into
			merged := reflect.New(mapElemType).Elem()
			if baseVal.IsValid() {
				merged.Set(baseVal)
			}
			mergeStruct(merged, overlayVal)
			base.SetMapIndex(key, merged)
		} else {
			// For non-struct map values, just overwrite
			base.SetMapIndex(key, overlayVal)
		}
	}
}

// applyLocale copies loaded locale values into package-level variables
// so existing call sites (messages.TitleAdded, messages.FieldUploader, etc.)
// continue to work without modification.
func applyLocale(l *Locale) {
	// Errors
	ErrorNotInVoiceChannel = l.Errors.NotInVoiceChannel
	ErrorEmptyQueue = l.Errors.EmptyQueue
	ErrorSongNotFound = l.Errors.SongNotFound
	ErrorPermissionDenied = l.Errors.PermissionDenied
	ErrorAdminOnly = l.Errors.AdminOnly
	ErrorAlreadyVoted = l.Errors.AlreadyVoted
	ErrorDuplicateSong = l.Errors.DuplicateSong

	// Titles
	TitleAdded = l.Titles.Added
	TitleSuccess = l.Titles.Success
	TitleRemoved = l.Titles.Removed
	TitleSkipped = l.Titles.Skipped
	TitleResumed = l.Titles.Resumed
	TitlePaused = l.Titles.Paused
	TitleRepeatAll = l.Titles.RepeatAll
	TitleRepeatSingle = l.Titles.RepeatSingle
	TitleRepeatOff = l.Titles.RepeatOff
	TitleSearching = l.Titles.Searching
	TitleLoading = l.Titles.Loading
	TitleNowPlaying = l.Titles.NowPlaying
	TitleQueue = l.Titles.Queue
	TitleHelp = l.Titles.Help
	TitleSearchResults = l.Titles.SearchResults
	TitlePlaylistFound = l.Titles.PlaylistFound
	TitlePlaylistAdded = l.Titles.PlaylistAdded
	TitlePlaylistStart = l.Titles.PlaylistStart
	TitleSkipVote = l.Titles.SkipVote
	TitleStopVote = l.Titles.StopVote
	TitleSystemInfo = l.Titles.SystemInfo
	TitleWarning = l.Titles.Warning
	TitleDuplicate = l.Titles.Duplicate
	TitleUnavailable = l.Titles.Unavailable
	TitleError = l.Titles.Error
	TitleEmptyQueue = l.Titles.EmptyQueue
	TitleNoSong = l.Titles.NoSong
	TitleNoPermission = l.Titles.NoPermission
	TitleAlreadyVoted = l.Titles.AlreadyVoted

	// Fields
	FieldUploader = l.Fields.Uploader
	FieldDuration = l.Fields.Duration
	FieldRequester = l.Fields.Requester
	FieldNextSong = l.Fields.NextSong
	FieldTotalSongs = l.Fields.TotalSongs
	FieldCurrentVote = l.Fields.CurrentVote
	FieldRequiredVote = l.Fields.RequiredVote
	FieldVoteResult = l.Fields.VoteResult
	FieldRemovedSongs = l.Fields.RemovedSongs
	FieldCurrentPrefix = l.Fields.CurrentPrefix
	FieldTotalCommands = l.Fields.TotalCommands
	FieldCPUInfo = l.Fields.CPUInfo
	FieldCPUUsage = l.Fields.CPUUsage
	FieldTotalMemory = l.Fields.TotalMemory
	FieldMemoryUsage = l.Fields.MemoryUsage
	FieldBotMemory = l.Fields.BotMemory
	FieldServerMemory = l.Fields.ServerMemory
	FieldPlayingServers = l.Fields.PlayingServers

	// Descriptions
	DescSearching = l.Descriptions.Searching
	DescLoading = l.Descriptions.Loading
	DescPlaylistConfirm = l.Descriptions.PlaylistConfirm
	DescPlaylistAdding = l.Descriptions.PlaylistAdding
	DescPlaylistSkipped = l.Descriptions.PlaylistSkipped
	DescVoteAdded = l.Descriptions.VoteAdded
	DescSkipped = l.Descriptions.Skipped
	DescPaused = l.Descriptions.Paused
	DescResumed = l.Descriptions.Resumed
	DescRepeatAll = l.Descriptions.RepeatAll
	DescRepeatSingle = l.Descriptions.RepeatSingle
	DescRepeatOff = l.Descriptions.RepeatOff
	DescRepeatOffRemoved = l.Descriptions.RepeatOffRemoved
	DescSongsRemoved = l.Descriptions.SongsRemoved
	DescEmptyQueue = l.Descriptions.EmptyQueue
	DescSearchPrompt = l.Descriptions.SearchPrompt
	DescSystemStatus = l.Descriptions.SystemStatus

	// Footers
	FooterPagination = l.Footers.Pagination
	FooterHelpPagination = l.Footers.HelpPagination
	FooterPlaylistConfirm = l.Footers.PlaylistConfirm
	FooterRequestedBy = l.Footers.RequestedBy
	FooterVoteReaction = l.Footers.VoteReaction

	// Buttons
	ButtonPrevious = l.Buttons.Previous
	ButtonNext = l.Buttons.Next

	// Select menus
	SelectPlaceholder = l.SelectMenus.Placeholder

	// Votes
	VoteMore = l.Votes.More

	// Help
	HelpCommandFormat = l.Help.CommandFormat
	HelpAdminMarker = l.Help.AdminMarker
}
