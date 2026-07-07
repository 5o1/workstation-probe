package handlers

import (
	"encoding/json"
	"net/http"
)

// writeJSON encodes v as the response body. When status is non-zero it is
// used as the HTTP status code (otherwise http.ResponseWriter writes 200
// implicitly). When noStore is true the response also carries
// Cache-Control: no-store, which the /metrics and /profile endpoints set
// because their contents change on every tick.
//
// Encoding errors are intentionally swallowed: the response is already
// partially on the wire by the time the encoder returns, so there's no
// useful recovery path. SetEscapeHTML(false) keeps characters like < > &
// readable in field values.
func writeJSON(w http.ResponseWriter, status int, v any, noStore bool) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	if noStore {
		h.Set("Cache-Control", "no-store")
	}
	if status != 0 {
		w.WriteHeader(status)
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	//nolint:errcheck // partial response already on the wire by the time Encode errors; recovery is impossible.
	_ = enc.Encode(v)
}
