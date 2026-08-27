package discord

import (
	"strconv"
	"time"
)

func NewComponentToken() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}
