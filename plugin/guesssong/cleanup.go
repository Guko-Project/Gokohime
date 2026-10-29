package guesssong

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/colanns/gokohime/internal/database"
	log "github.com/colanns/gokohime/internal/log"
	"gorm.io/gorm"
)

const (
	audioCacheTTL   = 30 * time.Minute
	gameTTL         = 30 * time.Minute
	cleanupInterval = 5 * time.Minute
)

// StartCacheMaintenance removes expired files at startup and periodically even
// when no new games are played. It never contacts the music service.
func StartCacheMaintenance(ctx context.Context) {
	run := func() {
		if err := songs.cleanup(ctx, time.Now()); err != nil {
			log.Warnf("[guesssong] cleanup failed: %v", err)
		}
	}
	run()
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}

func readDirIfPresent(path string) ([]os.DirEntry, error) {
	stat, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !stat.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", path)
	}
	return os.ReadDir(path)
}

func sessionIDFromDir(name string) (int64, bool) {
	var prefix string
	switch {
	case strings.HasPrefix(name, "group_"):
		prefix = "group_"
	case strings.HasPrefix(name, "private_"):
		prefix = "private_"
	default:
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(name, prefix), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	if prefix == "private_" {
		id = -id
	}
	return id, true
}

// Delete only plugin-owned filenames, never arbitrary paths stored in the DB.
func removeSliceFiles(dir string) error {
	files, err := readDirIfPresent(dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, file := range files {
		name := file.Name()
		if file.IsDir() || (name != "slice_1.wav" && name != "slice_2.wav" && name != "temp.mp3" && !(strings.HasPrefix(name, ".slice-") && strings.HasSuffix(name, ".wav"))) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	if remaining, err := os.ReadDir(dir); err == nil && len(remaining) == 0 {
		if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *songService) removeSessionFiles(name string) {
	if _, ok := sessionIDFromDir(name); !ok {
		return
	}
	for _, dir := range []string{filepath.Join(s.root, "sessions", name), filepath.Join(s.root, name)} {
		if err := removeSliceFiles(dir); err != nil {
			log.Warnf("[guesssong] remove session audio: %v", err)
		}
	}
}

func (s *songService) cleanup(ctx context.Context, now time.Time) error {
	var errs []error
	files, err := readDirIfPresent(filepath.Join(s.root, "cache"))
	if err != nil {
		errs = append(errs, err)
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		if !file.Type().IsRegular() {
			continue
		}
		name := file.Name()
		id, parseErr := strconv.ParseInt(strings.TrimSuffix(name, ".mp3"), 10, 64)
		temporary := strings.HasPrefix(name, ".download-") && strings.HasSuffix(name, ".mp3")
		if !temporary && (parseErr != nil || id <= 0 || !strings.HasSuffix(name, ".mp3")) {
			continue
		}
		remove := func() {
			path := filepath.Join(s.root, "cache", name)
			stat, err := os.Lstat(path)
			if os.IsNotExist(err) {
				return
			}
			if err != nil {
				errs = append(errs, err)
				return
			}
			if !stat.Mode().IsRegular() || stat.ModTime().Add(audioCacheTTL).After(now) {
				return
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
		}
		if temporary {
			remove()
			continue
		}
		lock := s.locks[uint64(id)%uint64(len(s.locks))]
		select {
		case <-lock:
			remove()
			lock <- struct{}{}
		default: // Currently downloading or slicing; defer to the next sweep.
		}
	}
	// Include the old per-session layout used before the shared cache existed.
	for _, root := range []string{filepath.Join(s.root, "sessions"), s.root} {
		dirs, err := readDirIfPresent(root)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, dir := range dirs {
			if !dir.IsDir() {
				continue
			}
			id, ok := sessionIDFromDir(dir.Name())
			if !ok {
				continue
			}
			if _, busy := busySessions.LoadOrStore(id, struct{}{}); busy {
				continue
			}
			func() {
				defer busySessions.Delete(id)
				session, err := database.LoadGuessGameSession(ctx, nil, id)
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					errs = append(errs, err)
					return
				}
				if err == nil && session.Active {
					if session.PreparedAt.Add(gameTTL).After(now) {
						return
					}
					if err := database.ClearGuessGameSession(ctx, nil, id); err != nil {
						errs = append(errs, err)
						return
					}
				}
				if err := removeSliceFiles(filepath.Join(root, dir.Name())); err != nil {
					errs = append(errs, err)
				}
			}()
		}
	}
	s.mu.Lock()
	for id, until := range s.failedUntil {
		if !until.After(now) {
			delete(s.failedUntil, id)
		}
	}
	s.mu.Unlock()
	return errors.Join(errs...)
}
