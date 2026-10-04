package ids

import (
	"strconv"
	"strings"
)

const (
	fnvOffset64 = 0xcbf29ce484222325
	fnvPrime64  = 0x100000001b3

	MaxNodeID int64 = 0x1fffffffffffff
)

func Stable(key string) int64 {
	hash := uint64(fnvOffset64)
	for index := 0; index < len(key); index++ {
		hash ^= uint64(key[index])
		hash *= fnvPrime64
	}
	return int64(hash & uint64(MaxNodeID))
}

func CypherString(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`'`, `\'`,
		"\n", `\n`,
		"\r", `\r`,
	)
	return "'" + replacer.Replace(value) + "'"
}

func CypherLiteral(value any) string {
	switch typed := value.(type) {
	case string:
		return CypherString(typed)
	case bool:
		return strconv.FormatBool(typed)
	case int:
		return strconv.Itoa(typed)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	}
	panic("ids: unsupported cypher literal")
}
