package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// CanonicalDigest produces a stable size-aware provenance pointer for JSON-like
// values. It never returns the original payload.
func CanonicalDigest(value any) (string, int64, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", 0, fmt.Errorf("canonical encode audit value: %w", err)
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), int64(len(raw)), nil
}
