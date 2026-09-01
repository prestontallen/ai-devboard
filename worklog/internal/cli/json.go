package cli

import (
	"encoding/json"
	"io"
)

// emitJSON writes v as indented JSON with a trailing newline. Used by every
// subcommand's --json output path.
func emitJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// jsonError is the canonical error shape for --json mode. Errors are sent to
// the same stdout stream as success output so callers can parse a single
// JSON document regardless of outcome.
type jsonError struct {
	Error string `json:"error"`
}
