package configstate

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"sort"
	"strconv"
)

// BytesRevision returns the SHA-256 revision of configuration bytes.
func BytesRevision(data []byte) Revision {
	h := sha256.New()
	writeFrame(h, "bytes")
	writeFrame(h, string(data))
	return Revision(hex.EncodeToString(h.Sum(nil)))
}

// AggregateRevision returns the SHA-256 revision of sorted logical parts.
func AggregateRevision(parts []RevisionPart) Revision {
	ordered := append([]RevisionPart(nil), parts...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Key != ordered[j].Key {
			return ordered[i].Key < ordered[j].Key
		}
		if ordered[i].State != ordered[j].State {
			return ordered[i].State < ordered[j].State
		}
		return ordered[i].Revision < ordered[j].Revision
	})

	h := sha256.New()
	writeFrame(h, "aggregate")
	for _, part := range ordered {
		writeFrame(h, "part")
		writeFrame(h, part.Key)
		writeFrame(h, string(part.State))
		writeFrame(h, string(part.Revision))
	}
	return Revision(hex.EncodeToString(h.Sum(nil)))
}

// CandidateRevision combines a domain revision with the revisions it validated against.
func CandidateRevision(own Revision, dependencies map[Source]Revision) Revision {
	parts := make([]RevisionPart, 0, len(dependencies)+1)
	parts = append(parts, RevisionPart{Key: "candidate", State: Valid, Revision: own})
	for source, revision := range dependencies {
		parts = append(parts, RevisionPart{
			Key:      "dependency/" + string(source),
			State:    Valid,
			Revision: revision,
		})
	}
	return AggregateRevision(parts)
}

func writeFrame(h hash.Hash, value string) {
	_, _ = h.Write([]byte(strconv.Itoa(len(value))))
	_, _ = h.Write([]byte{':'})
	_, _ = h.Write([]byte(value))
	_, _ = h.Write([]byte{0})
}
