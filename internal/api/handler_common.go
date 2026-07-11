package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Resinat/Resin/internal/service"
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
// It accepts a comma-separated list of protocol names (case-insensitive).
// Returns the canonical protocol names and a boolean indicating success.
// On invalid protocol values, it writes an INVALID_ARGUMENT error and returns false.
func parseProtocolQuery(w http.ResponseWriter, q url.Values) ([]string, bool) {
	raw := q.Get("protocol")
	if raw == "" {
		return nil, true
	}
	parts := strings.Split(raw, ",")
	canonical := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		c := service.NormalizeProtocol(p)
		if c == "" {
			writeInvalidArgument(w, "protocol: unsupported value '"+p+"'; supported: shadowsocks, ss, vmess, vmess1, trojan, vless, hysteria2, hy2, http, socks, socks5")
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
	if len(canonical) == 0 {
		return nil, true
	}
	return canonical, true
}
