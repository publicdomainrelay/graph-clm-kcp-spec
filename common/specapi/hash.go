package specapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func HashJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("specapi: encode for hashing: %w", err)
	}
	return HashBytes(encoded), nil
}

func HashBytes(encoded []byte) string {
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func IsHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
