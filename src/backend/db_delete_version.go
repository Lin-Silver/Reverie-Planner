package main

import (
	"errors"
	"net/http"
	"strconv"
)

// parseOptionalDeleteVersion keeps versionless legacy DELETE requests valid,
// but rejects an explicitly empty or ambiguous version instead of treating
// it as an unversioned write. A stale client must not bypass conflict checks
// with ?version= or duplicate version parameters.
func parseOptionalDeleteVersion(r *http.Request) (int, error) {
	values, present := r.URL.Query()["version"]
	if !present {
		return 0, nil
	}
	if len(values) != 1 || values[0] == "" {
		return 0, errors.New("invalid delete version")
	}
	version, err := strconv.Atoi(values[0])
	if err != nil || version < 1 {
		return 0, errors.New("invalid delete version")
	}
	return version, nil
}
