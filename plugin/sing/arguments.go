package sing

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var errTimeRange = errors.New("invalid time range")

const secondsPattern = `[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?`

var timeRangePattern = regexp.MustCompile(`^(` + secondsPattern + `)-(` + secondsPattern + `)$`)

// Only consume a standalone final token, preserving all internal lyric newlines.
func parseTimeRange(query string) (string, *float64, *float64, error) {
	query = strings.TrimSpace(query)
	i := strings.LastIndexFunc(query, unicode.IsSpace)
	token := query
	if i >= 0 {
		token = strings.TrimSpace(query[i:])
	}
	parts := timeRangePattern.FindStringSubmatch(token)
	if parts == nil {
		return query, nil, nil, nil
	}
	start, startErr := strconv.ParseFloat(parts[1], 64)
	end, endErr := strconv.ParseFloat(parts[2], 64)
	if startErr != nil || endErr != nil || math.IsInf(end-start, 0) || start < 0 || end <= start {
		return "", nil, nil, errTimeRange
	}
	if i < 0 || strings.TrimSpace(query[:i]) == "" {
		return "", nil, nil, errEmptyQuery
	}
	return strings.TrimSpace(query[:i]), &start, &end, nil
}
