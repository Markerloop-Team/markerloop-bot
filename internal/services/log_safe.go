package services

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// tokenFingerprint — безопасный для журнала признак токена из /start:
// длина и первые 8 hex sha256. Сам аргумент /start — одноразовый токен
// входа и одновременно текст пользователя, в журнал не пишется (SDL-738).
func tokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "len=" + strconv.Itoa(len(token)) + " fp=" + hex.EncodeToString(sum[:4])
}
