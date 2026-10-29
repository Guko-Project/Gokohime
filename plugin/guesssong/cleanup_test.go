package guesssong

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/colanns/gokohime/internal/database"
)

func putFileAt(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func assertFileExists(t *testing.T, path string, want bool) {
	t.Helper()
	_, err := os.Stat(path)
	if want && err != nil {
		t.Fatalf("expected %s: %v", path, err)
	}
	if !want && !os.IsNotExist(err) {
		t.Fatalf("expected removed %s: %v", path, err)
	}
}

func TestCacheCleanupRespectsAgeAndInUseAudio(t *testing.T) {
	gameDB(t)
	root := t.TempDir()
	svc := newSongService(root, &neteaseClient{})
	now := time.Now()
	old := now.Add(-audioCacheTTL - time.Minute)
	for _, name := range []string{"1.mp3", "2.mp3", ".download-old.mp3", "keep.txt"} {
		putFileAt(t, filepath.Join(root, "cache", name), old)
	}
	putFileAt(t, filepath.Join(root, "cache", "3.mp3"), now)
	putFileAt(t, filepath.Join(root, "cache", ".download-new.mp3"), now)
	release, err := svc.lockSong(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.cleanup(context.Background(), now); err != nil {
		release()
		t.Fatal(err)
	}
	release()
	for _, name := range []string{"2.mp3", "3.mp3", ".download-new.mp3", "keep.txt"} {
		assertFileExists(t, filepath.Join(root, "cache", name), true)
	}
	for _, name := range []string{"1.mp3", ".download-old.mp3"} {
		assertFileExists(t, filepath.Join(root, "cache", name), false)
	}
	if err := svc.cleanup(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	assertFileExists(t, filepath.Join(root, "cache", "2.mp3"), false)
}

func TestCacheHitRefreshesRetention(t *testing.T) {
	gameDB(t)
	root := t.TempDir()
	path := filepath.Join(root, "cache", "1.mp3")
	putFileAt(t, path, time.Now())
	if err := os.WriteFile(path, audioFixture(t), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * audioCacheTTL)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	svc := newSongService(root, &neteaseClient{})
	if _, _, err := svc.cachedAudio(context.Background(), 1, 20000); err != nil {
		t.Fatal(err)
	}
	if err := svc.cleanup(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	assertFileExists(t, path, true)
}

func TestSessionCleanupPreservesActiveAndBusyGames(t *testing.T) {
	db := gameDB(t)
	root := t.TempDir()
	svc := newSongService(root, &neteaseClient{})
	now := time.Now()
	for _, session := range []database.GuessGameSession{
		{GroupID: 1, Active: true, PreparedAt: now},
		{GroupID: 2, Active: true, PreparedAt: now.Add(-gameTTL - time.Minute)},
	} {
		if err := db.Create(&session).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"group_1", "group_2", "group_3", "private_4"} {
		putFileAt(t, filepath.Join(root, "sessions", name, "slice_1.wav"), now)
	}
	putFileAt(t, filepath.Join(root, "group_5", "temp.mp3"), now)
	putFileAt(t, filepath.Join(root, "sessions", "private_4", "notes.txt"), now)
	busySessions.Store(int64(3), struct{}{})
	t.Cleanup(func() { busySessions.Delete(int64(3)) })
	if err := svc.cleanup(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	assertFileExists(t, filepath.Join(root, "sessions", "group_1", "slice_1.wav"), true)
	assertFileExists(t, filepath.Join(root, "sessions", "group_3", "slice_1.wav"), true)
	assertFileExists(t, filepath.Join(root, "sessions", "group_2", "slice_1.wav"), false)
	assertFileExists(t, filepath.Join(root, "sessions", "private_4", "slice_1.wav"), false)
	assertFileExists(t, filepath.Join(root, "sessions", "private_4", "notes.txt"), true)
	assertFileExists(t, filepath.Join(root, "group_5", "temp.mp3"), false)
	expired, err := database.LoadGuessGameSession(context.Background(), nil, 2)
	if err != nil || expired.Active {
		t.Fatalf("expired game still active: %+v %v", expired, err)
	}
}
