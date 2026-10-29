package guesssong

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/colanns/gokohime/internal/database"
)

// Merged playlist and legacy metadata snapshots, fetched on 2026-10-30.
// No audio or expiring playback URLs are embedded.
//
//go:embed catalog_cv.json
var defaultCatalogJSON []byte

//go:embed catalog_rv.json
var defaultRVCatalogJSON []byte

type catalogTrack struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Artists    []string `json:"artists"`
	DurationMS int      `json:"duration_ms"`
}

// StartupCheck merges bundled metadata into both libraries, including existing
// installations. It performs no network I/O and never downloads audio.
func StartupCheck(ctx context.Context) error {
	for _, source := range []struct {
		library string
		data    []byte
	}{
		{"cv", defaultCatalogJSON}, {"rv", defaultRVCatalogJSON},
	} {
		if err := mergeCatalog(ctx, source.library, source.data); err != nil {
			return fmt.Errorf("merge %s catalog: %w", source.library, err)
		}
	}
	return nil
}

func mergeCatalog(ctx context.Context, library string, data []byte) error {
	var catalog struct {
		Tracks []catalogTrack `json:"tracks"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		return fmt.Errorf("decode bundled playlist: %w", err)
	}
	entries := make([]database.GuessSongCatalog, 0, len(catalog.Tracks))
	seen := make(map[int64]bool)
	for _, track := range catalog.Tracks {
		if track.ID <= 0 {
			return fmt.Errorf("invalid song ID %d", track.ID)
		}
		if seen[track.ID] {
			continue
		}
		seen[track.ID] = true
		meta, err := json.Marshal(track)
		if err != nil {
			return err
		}
		entries = append(entries, database.GuessSongCatalog{SongID: track.ID, MetaJSON: string(meta)})
	}
	return database.MergeGuessSongCatalog(ctx, nil, library, entries)
}
