package youtube

import (
	"fmt"
	"strings"

	"noraegaori/internal/messages"
)

type Restriction int

const (
	RestrictionNone Restriction = iota
	RestrictionPrivateVideo
	RestrictionAgeRestricted
	RestrictionGeoRestricted
	RestrictionMembersOnly
	RestrictionPremiumOnly
	RestrictionCopyright
	RestrictionUnplayable
	RestrictionDeletedVideo
	RestrictionUnavailable
	RestrictionContentCheck
)

func parseRestriction(status, reason string, msgs []string) Restriction {
	reasonLower := strings.ToLower(reason)
	messagesStr := strings.ToLower(strings.Join(msgs, " "))

	switch status {
	case "LOGIN_REQUIRED":
		if strings.Contains(reasonLower, "private") || strings.Contains(messagesStr, "private") {
			return RestrictionPrivateVideo
		}
		return RestrictionAgeRestricted

	case "UNPLAYABLE":
		switch {
		case strings.Contains(reasonLower, "country") || strings.Contains(reasonLower, "region"):
			return RestrictionGeoRestricted
		case strings.Contains(reasonLower, "members") || strings.Contains(reasonLower, "membership"):
			return RestrictionMembersOnly
		case strings.Contains(reasonLower, "premium"):
			return RestrictionPremiumOnly
		case strings.Contains(reasonLower, "copyright"):
			return RestrictionCopyright
		}
		return RestrictionUnplayable

	case "ERROR":
		if strings.Contains(reasonLower, "unavailable") {
			return RestrictionDeletedVideo
		}
		return RestrictionUnavailable

	case "CONTENT_CHECK_REQUIRED":
		return RestrictionContentCheck

	default:
		if reason != "" {
			return RestrictionUnplayable
		}
		return RestrictionUnavailable
	}
}

func (r Restriction) Message(guildID, reason string) string {
	yt := &messages.T(guildID).YouTube

	switch r {
	case RestrictionPrivateVideo:
		return yt.ErrorPrivateVideo
	case RestrictionAgeRestricted:
		return yt.ErrorAgeRestricted
	case RestrictionGeoRestricted:
		return yt.ErrorGeoRestricted
	case RestrictionMembersOnly:
		return yt.ErrorMembersOnly
	case RestrictionPremiumOnly:
		return yt.ErrorPremiumOnly
	case RestrictionCopyright:
		return yt.ErrorCopyright
	case RestrictionUnplayable:
		if reason != "" {
			return fmt.Sprintf(yt.ErrorUnplayableReason, reason)
		}
		return yt.ErrorUnplayable
	case RestrictionDeletedVideo:
		return yt.ErrorDeletedVideo
	case RestrictionContentCheck:
		return yt.ErrorContentCheck
	}
	if reason != "" {
		return fmt.Sprintf(yt.ErrorUnavailableReason, reason)
	}
	return yt.ErrorUnavailable
}

func (r Restriction) IsDefinitive() bool {
	return r != RestrictionNone && r != RestrictionAgeRestricted
}
