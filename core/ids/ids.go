package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// EditionID is stable for one build attempt and is not derived from the clock alone.
func EditionID() (string, error) {
	id, err := ulid()
	if err != nil {
		return "", err
	}
	return "ed_" + id, nil
}

func Prefixed(prefix string) (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b[:]), nil
}

func ulid() (string, error) {
	var raw [16]byte
	ms := time.Now().UnixMilli()
	raw[0] = byte(ms >> 40)
	raw[1] = byte(ms >> 32)
	raw[2] = byte(ms >> 24)
	raw[3] = byte(ms >> 16)
	raw[4] = byte(ms >> 8)
	raw[5] = byte(ms)
	if _, err := rand.Read(raw[6:]); err != nil {
		return "", err
	}
	return encodeCrockford(raw[:]), nil
}

func encodeCrockford(in []byte) string {
	var n uint64
	bits := 0
	out := make([]byte, 0, 26)
	for _, b := range in {
		n = (n << 8) | uint64(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out = append(out, crockford[(n>>bits)&31])
		}
	}
	if bits > 0 {
		out = append(out, crockford[(n<<(5-bits))&31])
	}
	return string(out)
}

func Hash(parts ...any) (string, error) {
	raw, err := json.Marshal(parts)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// ChunkKey is the logical identity. Line numbers and edition ids are not inputs.
func ChunkKey(kb, path string, heading []string, text string, dup int) (string, error) {
	sum, err := Hash("id_rule=2", kb, path, heading, text, dup)
	if err != nil {
		return "", err
	}
	return "k" + sum[:32], nil
}

// ChunkID is the physical id of one chunk inside one edition.
func ChunkID(editionID, chunkKey string) (string, error) {
	sum, err := Hash(editionID, chunkKey)
	if err != nil {
		return "", err
	}
	b, err := hex.DecodeString(sum[:32])
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

type Ref struct {
	Origin  string `json:"o"`
	KB      string `json:"k"`
	Path    string `json:"p"`
	Edition string `json:"e"`
	Chunk   string `json:"c,omitempty"`
}

func EncodeRef(r Ref) (string, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return "xr1:" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeRef(s string) (Ref, error) {
	const prefix = "xr1:"
	if len(s) <= len(prefix) || s[:len(prefix)] != prefix {
		return Ref{}, fmt.Errorf("invalid ref")
	}
	raw, err := base64.RawURLEncoding.DecodeString(s[len(prefix):])
	if err != nil {
		return Ref{}, err
	}
	var r Ref
	if err := json.Unmarshal(raw, &r); err != nil {
		return Ref{}, err
	}
	if r.KB == "" || r.Edition == "" || r.Path == "" {
		return Ref{}, fmt.Errorf("incomplete ref")
	}
	return r, nil
}
