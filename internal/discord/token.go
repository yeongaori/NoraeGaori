package discord

import (
	"crypto/rand"
	"encoding/hex"
)

func NewComponentToken() string {
	buffer := make([]byte, 8)
	rand.Read(buffer)
	return hex.EncodeToString(buffer)
}
