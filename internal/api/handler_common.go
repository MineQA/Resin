package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Resinat/Resin/internal/cloudflare"
	"github.com/Resinat/Resin/internal/node"
)

func parsePaginationOrWriteInvalid(w http.ResponseWriter, r *http.Request) (Pagination, bool) {
	pg, err := ParsePagination(r)
	if err != nil {
		writeInvalidArgument(w, err.Error())
		return Pagination{}, false
	}
	return pg, true
}

func parseSortingOrWriteInvalid(
	w http.ResponseWriter,
	r *http.Request,
	allowed []string,
	defaultField string,
	defaultOrder string,
) (Sorting, bool) {
	s, err := ParseSorting(r, allowed, defaultField, defaultOrder)
	if err != nil {
		writeInvalidArgument(w, err.Error())
		return Sorting{}, false
	}
	return s, true
}

func parseBoolQueryOrWriteInvalid(w http.ResponseWriter, r *http.Request, key string) (*bool, bool) {
	v, err := ParseBoolQuery(r, key)
	if err != nil {
		writeInvalidArgument(w, err.Error())
		return nil, false
	}
	return v, true
}

func readRawBodyOrWriteInvalid(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.Body == nil {
		writeInvalidArgument(w, "request body is required")
		return nil, false
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writePayloadTooLarge(w, maxErr.Limit)
			return nil, false
		}
		writeInvalidArgument(w, "failed to read body")
		return nil, false
	}
	return body, true
}

func requireUUIDPathParam(
	w http.ResponseWriter,
	r *http.Request,
	paramName string,
	fieldName string,
) (string, bool) {
	value := PathParam(r, paramName)
	if !ValidateUUID(value) {
		writeInvalidArgument(w, fmt.Sprintf("%s: must be a valid UUID", fieldName))
		return "", false
	}
	return value, true
}

func parseOptionalUUIDQuery(
	w http.ResponseWriter,
	r *http.Request,
	queryKey string,
	fieldName string,
) (*string, bool) {
	value := r.URL.Query().Get(queryKey)
	if value == "" {
		return nil, true
	}
	if !ValidateUUID(value) {
		writeInvalidArgument(w, fmt.Sprintf("%s: must be a valid UUID", fieldName))
		return nil, false
	}
	return &value, true
}

func applySortOrder(order int, sortOrder string) int {
	if sortOrder == "desc" {
		return -order
	}
	return order
}

// parseProtocolQuery parses the "protocol" query parameter.
// It accepts comma-separated lists and/or repeated query values (case-insensitive).
// Returns the canonical protocol names and a boolean indicating success.
// On invalid protocol values, it writes an INVALID_ARGUMENT error and returns false.
func parseProtocolQuery(w http.ResponseWriter, q url.Values) ([]string, bool) {
	rawValues, ok := q["protocol"]
	if !ok || len(rawValues) == 0 {
		return nil, true
	}
	return parseProtocolValues(w, rawValues, "protocol")
}

// parseExcludeProtocolQuery parses the "exclude_protocol" (canonical) or
// "protocol_exclude" (alias) query parameter.
// It accepts comma-separated lists and/or repeated query values (case-insensitive).
// Returns the canonical protocol names and a boolean indicating success.
// On invalid protocol values, it writes an INVALID_ARGUMENT error and returns false.
func parseExcludeProtocolQuery(w http.ResponseWriter, q url.Values) ([]string, bool) {
	rawValues, ok := q["exclude_protocol"]
	if !ok || len(rawValues) == 0 {
		rawValues, ok = q["protocol_exclude"]
	}
	if !ok || len(rawValues) == 0 {
		return nil, true
	}
	return parseProtocolValues(w, rawValues, "exclude_protocol")
}

// parseProtocolValues is the shared implementation for parsing protocol filter
// query values. The paramName is used in error messages.
func parseProtocolValues(w http.ResponseWriter, rawValues []string, paramName string) ([]string, bool) {
	canonical := make([]string, 0, len(rawValues)*2)
	for _, raw := range rawValues {
		if raw == "" {
			continue
		}
		parts := strings.Split(raw, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			c := node.NormalizeProtocol(p)
			if c == "" {
				writeInvalidArgument(w, fmt.Sprintf("%s: unsupported value '%s'; supported: %v", paramName, p, node.CanonicalProtocols))
				return nil, false
			}
			// Deduplicate.
			seen := false
			for _, existing := range canonical {
				if existing == c {
					seen = true
					break
				}
			}
			if !seen {
				canonical = append(canonical, c)
			}
		}
	}
	if len(canonical) == 0 {
		return nil, true
	}
	return canonical, true
}

// parseCloudflareStatusesQuery reads all repeated quality_cloudflare_status
// query values, validates each token, and returns them normalized and
// deduplicated. Unknown or empty explicit tokens return 400.
func parseCloudflareStatusesQuery(w http.ResponseWriter, q url.Values) ([]string, bool) {
	values := q["quality_cloudflare_status"]
	if len(values) == 0 {
		return nil, true // no filter
	}
	normalized, err := cloudflare.NormalizeSet(values)
	if err != nil {
		writeInvalidArgument(w, err.Error())
		return nil, false
	}
	return normalized, true
}
