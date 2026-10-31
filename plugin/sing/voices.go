package sing

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const defaultVoiceID = "vocaloid-luotianyi"

var errVoiceUnavailable = errors.New("voice unavailable")
var errEmptyQuery = errors.New("voice selected without song or lyrics")
var errLyricMatch = errors.New("complete lyrics not matched")

type voice struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Aliases   []string `json:"aliases"`
	Available bool     `json:"available"`
}
type voiceCatalog struct {
	Voices []voice `json:"voices"`
}
type catalogKey struct{ base, key string }
type cachedCatalog struct {
	catalog voiceCatalog
	expires time.Time
}

var catalogs = struct {
	sync.Mutex
	entries map[catalogKey]cachedCatalog
}{entries: make(map[catalogKey]cachedCatalog)}

func (c *client) voices(ctx context.Context) (voiceCatalog, error) {
	catalogs.Lock()
	key := catalogKey{c.baseURL, c.apiKey}
	if cached, ok := catalogs.entries[key]; ok && time.Now().Before(cached.expires) {
		catalogs.Unlock()
		return cached.catalog, nil
	}
	catalogs.Unlock()
	var result voiceCatalog
	if err := c.json(ctx, http.MethodGet, "/voices", nil, &result); err != nil {
		return result, err
	}
	if len(result.Voices) == 0 {
		return result, errors.New("empty voice catalog")
	}
	catalogs.Lock()
	defer catalogs.Unlock()
	if len(catalogs.entries) >= 32 {
		clear(catalogs.entries)
	}
	catalogs.entries[key] = cachedCatalog{result, time.Now().Add(5 * time.Minute)}
	return result, nil
}

// Only consume an exact alias at the beginning followed by whitespace (or EOF).
// Unknown prefixes preserve the complete query, including internal line breaks.
func selectVoice(query string, catalog voiceCatalog) (voice, string, error) {
	query = strings.TrimSpace(query)
	type candidate struct {
		alias string
		voice voice
	}
	var candidates []candidate
	var chosen voice
	for _, v := range catalog.Voices {
		if v.ID == defaultVoiceID {
			chosen = v
		}
		for _, alias := range append([]string{v.ID, v.Name}, v.Aliases...) {
			if alias != "" {
				candidates = append(candidates, candidate{alias, v})
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return len(candidates[i].alias) > len(candidates[j].alias) })
	for _, item := range candidates {
		if len(query) < len(item.alias) || !strings.EqualFold(query[:len(item.alias)], item.alias) {
			continue
		}
		rest := query[len(item.alias):]
		if rest != "" {
			r, _ := utf8.DecodeRuneInString(rest)
			if !unicode.IsSpace(r) {
				continue
			}
		}
		chosen, query = item.voice, strings.TrimSpace(rest)
		break
	}
	if query == "" {
		return chosen, query, errEmptyQuery
	}
	if chosen.ID == "" || !chosen.Available {
		return chosen, query, errVoiceUnavailable
	}
	return chosen, query, nil
}
