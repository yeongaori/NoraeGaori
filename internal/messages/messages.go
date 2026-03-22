package messages

// Korean message constants matching Node.js NoraeDev bot

// Embed colors
const (
	ColorSuccess = 0x00FF00 // Bright Green - Playing/Success
	ColorInfo    = 0x0099FF // Blue - Queue/Help/Info
	ColorWarning = 0xFFA500 // Orange - Loading/Warning
	ColorError   = 0xFF0000 // Red - Error
	ColorPaused  = 0xFF6B35 // Orange-Red - Paused
)

// Common error messages (populated from locale JSON at startup, defaults as fallback)
var (
	ErrorNotInVoiceChannel = "먼저 음성 채널에 참가해야 합니다"
	ErrorEmptyQueue        = "현재 대기열에 노래가 없습니다"
	ErrorSongNotFound      = "노래를 찾을 수 없습니다"
	ErrorPermissionDenied  = "이 명령어는 봇 관리자만 사용할 수 있습니다"
	ErrorAdminOnly         = "이 명령어는 관리자만 사용할 수 있습니다"
	ErrorAlreadyVoted      = "이미 스킵 투표를 하셨습니다"
	ErrorDuplicateSong     = "이미 대기열에 있는 곡입니다"
)

// Title emojis and texts (populated from locale JSON at startup, defaults as fallback)
var (
	// Success
	TitleAdded    = "대기열에 추가됨"
	TitleSuccess  = "성공"
	TitleRemoved  = "제거 완료"
	TitleSkipped  = "스킵됨"
	TitleResumed  = "재개됨"
	TitlePaused   = "일시 정지"
	TitleRepeatAll    = "전체 반복 활성화"
	TitleRepeatSingle = "한곡 반복 활성화"
	TitleRepeatOff    = "반복 비활성화"

	// Loading
	TitleSearching = "검색 중..."
	TitleLoading   = "로딩 중..."

	// Info
	TitleNowPlaying      = "현재 재생 중"
	TitleQueue           = "음악 대기열"
	TitleHelp            = "봇 명령어 도움말"
	TitleSearchResults   = "검색 결과"
	TitlePlaylistFound   = "플레이리스트 발견"
	TitlePlaylistAdded   = "플레이리스트 추가 완료"
	TitlePlaylistStart   = "플레이리스트 재생 시작"
	TitleSkipVote        = "스킵 투표"
	TitleStopVote        = "정지 투표"
	TitleSystemInfo      = "시스템 정보"

	// Warnings
	TitleWarning         = "경고"
	TitleDuplicate       = "중복된 곡"
	TitleUnavailable     = "재생 불가능한 곡"

	// Errors
	TitleError           = "오류"
	TitleEmptyQueue      = "빈 대기열"
	TitleNoSong          = "재생할 노래 없음"
	TitleNoPermission    = "권한 없음"
	TitleAlreadyVoted    = "이미 투표함"
)

// Field names (populated from locale JSON at startup, defaults as fallback)
var (
	FieldUploader      = "업로더"
	FieldDuration      = "길이"
	FieldRequester     = "요청자"
	FieldNextSong      = "다음 곡"
	FieldTotalSongs    = "전체 곡 수"
	FieldCurrentVote   = "현재 투표"
	FieldRequiredVote  = "필요한 투표"
	FieldVoteResult    = "투표 결과"
	FieldRemovedSongs  = "제거된 곡"
	FieldCurrentPrefix = "현재 Prefix"
	FieldTotalCommands = "총 명령어"
	FieldCPUInfo       = "CPU 정보"
	FieldCPUUsage      = "CPU 사용량"
	FieldTotalMemory   = "총 메모리"
	FieldMemoryUsage   = "메모리 사용량"
	FieldBotMemory     = "봇 메모리 사용량"
	FieldServerMemory  = "현재 서버 메모리"
	FieldPlayingServers = "재생 중인 서버"
)

// Description templates (populated from locale JSON at startup, defaults as fallback)
var (
	DescSearching         = "`%s`를 검색하고 있습니다..."
	DescLoading           = "오디오를 로드하고 있습니다..."
	DescPlaylistConfirm   = "전체 플레이리스트(**%d**곡)를 대기열에 추가하시겠습니까?"
	DescPlaylistAdding    = "첫 번째 곡이 재생을 시작합니다.\n나머지 %d곡은 순서대로 대기열에 추가 중입니다..."
	DescPlaylistSkipped   = "%d곡은 재생할 수 없어 건너뛰었습니다."
	DescVoteAdded         = "스킵 투표가 추가되었습니다."
	DescSkipped           = "**%s**이(가) 스킵되었습니다."
	DescPaused            = "노래를 일시 정지하고 채널에서 나갔습니다. 대기열과 재생 위치는 보존됩니다."
	DescResumed           = "노래가 다시 재생됩니다."
	DescRepeatAll         = "대기열 반복이 **활성화됨**으로 설정되었습니다.\n현재 재생중인 곡이 끝나면 대기열에 다시 추가됩니다."
	DescRepeatSingle      = "한곡 반복이 **활성화됨**으로 설정되었습니다.\n현재 재생중인 곡이 끝나면 같은 곡이 다시 재생됩니다."
	DescRepeatOff         = "대기열 반복이 **비활성화됨**으로 설정되었습니다."
	DescRepeatOffRemoved  = "대기열 반복이 **비활성화됨**으로 설정되었습니다.\n대기열에서 중복된 %d곡이 제거되었습니다."
	DescSongsRemoved      = "**%d**곡이 대기열에서 제거되었습니다."
	DescEmptyQueue        = "현재 대기열이 비어있습니다."
	DescSearchPrompt      = "`%s`에 대한 검색 결과입니다. 재생할 노래를 선택하세요."
	DescSystemStatus      = "봇의 현재 시스템 상태"
)

// Footer templates (populated from locale JSON at startup, defaults as fallback)
var (
	FooterPagination        = "페이지 %d/%d | 총 %d곡"
	FooterHelpPagination    = "페이지 %d/%d"
	FooterPlaylistConfirm   = "✅ 반응으로 전체 추가 | 30초 후 자동 취소"
	FooterRequestedBy       = "요청자: %s"
	FooterVoteReaction      = "%s 반응으로도 투표 가능 | %d초 후 투표 만료"
)

// Button labels (populated from locale JSON at startup, defaults as fallback)
var (
	ButtonPrevious = "◀️ 이전"
	ButtonNext     = "다음 ▶️"
)

// Select menu (populated from locale JSON at startup, defaults as fallback)
var (
	SelectPlaceholder = "재생할 노래를 선택하세요"
)

// Vote messages (populated from locale JSON at startup, defaults as fallback)
var (
	VoteMore = "%d표 더"
)

// Help command text (populated from locale JSON at startup, defaults as fallback)
var (
	HelpCommandFormat = "**%d. %s**\n%s\n메시지: `%s%s%s`\n별칭: `%s`\n슬래시: `/%s`\n예시: `%s%s %s`\n\n"
	HelpAdminMarker   = "🔴 "
)
