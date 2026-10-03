package ids

import (
	"strconv"
	"strings"
)

const (
	offset64 = 0xcbf29ce484222325
	prime64  = 0x100000001b3

	MaxNodeID int64 = 0x1fffffffffffff
)

// Stable is FNV-1a over the key, masked to 53 bits so the value survives a
// round trip through a JSON number and through the CLM graph (same key, same
// id, in both this repo and pi-hydradb-clm). Keys are ASCII by construction.
func Stable(key string) int64 {
	hash := uint64(offset64)
	for index := 0; index < len(key); index++ {
		hash ^= uint64(key[index])
		hash *= prime64
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
