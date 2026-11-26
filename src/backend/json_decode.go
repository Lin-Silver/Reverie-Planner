package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// maxJSONBodyBytes bounds every JSON request body. The loopback security
// middleware also caps the body, but handlers that read the body themselves
// enforce the same limit so decoding is safe independent of middleware.
const maxJSONBodyBytes = 4 << 20

// decodeJSONBody is the shared normalization path for object- and array-shaped
// JSON payloads. It enforces the size limit, rejects duplicate object keys and
// trailing values at any nesting depth (see rejectDuplicateJSONKeys), then
// unmarshals into target. The concrete target type enforces the expected
// top-level shape: unmarshalling an array into a struct/map or an object into a
// slice fails and surfaces as a 400 at the call site.
//
// Callers that need object field presence (which keys were supplied) must use
// decodeJSONRequest instead; that helper keeps its field-map return contract.
func decodeJSONBody(r *http.Request, target any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBodyBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxJSONBodyBytes {
		return errors.New("request body is too large")
	}
	if err := rejectDuplicateJSONKeys(body); err != nil {
		return err
	}
	return json.Unmarshal(body, target)
}