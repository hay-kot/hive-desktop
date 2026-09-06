package queries

// maxEventDetailBytes bounds inbox_event.detail. It stays beside
// boundEventDetail rather than in models: both are private to
// IngestObservation (inbox_item.go) and never cross the package boundary.
const maxEventDetailBytes = 4096

func boundEventDetail(detail []byte) []byte {
	if len(detail) <= maxEventDetailBytes {
		return detail
	}
	return append([]byte(nil), detail[:maxEventDetailBytes]...)
}
