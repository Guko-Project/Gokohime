package guesssong

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colanns/gokohime/internal/database"
	"github.com/tidwall/gjson"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func gameDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "game.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&database.GuessSongCatalog{}, &database.GuessGameSession{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.SwapGlobalForTest(db))
	return db
}

func TestSeedCatalogMetadataOnly(t *testing.T) {
	db := gameDB(t)
	root := filepath.Join(t.TempDir(), "no-audio")
	old := songs
	songs = newSongService(root, &neteaseClient{}) // a network call would fail
	t.Cleanup(func() { songs = old })
	for i := 0; i < 2; i++ {
		if err := StartupCheck(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	var entries []database.GuessSongCatalog
	if err := db.Find(&entries).Error; err != nil {
		t.Fatal(err)
	}
	if len(entries) != 843+986 {
		t.Fatalf("catalog contains %d tracks", len(entries))
	}
	seen := make(map[string]bool)
	counts := make(map[string]int)
	missing := make(map[string]int)
	for _, entry := range entries {
		var track catalogTrack
		if err := json.Unmarshal([]byte(entry.MetaJSON), &track); err != nil {
			t.Fatal(err)
		}
		key := fmt.Sprintf("%s:%d", entry.Library, track.ID)
		if track.ID != entry.SongID || track.ID <= 0 || seen[key] || (entry.Library != "cv" && entry.Library != "rv") {
			t.Fatalf("invalid track: %+v", track)
		}
		if track.Name == "" {
			missing[entry.Library]++
		} else if track.DurationMS <= 0 || len(track.Artists) == 0 {
			t.Fatalf("incomplete metadata: %+v", track)
		}
		seen[key] = true
		counts[entry.Library]++
	}
	if counts["cv"] != 843 || counts["rv"] != 986 || missing["cv"] != 15 || missing["rv"] != 7 {
		t.Fatalf("counts=%v missing=%v", counts, missing)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("startup downloaded audio")
	}
	if err := db.Delete(&entries[0]).Error; err != nil {
		t.Fatal(err)
	}
	if err := StartupCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&database.GuessSongCatalog{}).Count(&count)
	if count != 843+986-1 {
		t.Fatal("deleted entry was restored")
	}
}

func TestSeedPreservesCustomLibrary(t *testing.T) {
	db := gameDB(t)
	entry := testTrack(1)
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if err := StartupCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&database.GuessSongCatalog{}).Count(&count)
	if count != 843+986+1 {
		t.Fatalf("existing library did not receive new tracks: %d", count)
	}
	var got database.GuessSongCatalog
	if err := db.Where("library = ? AND song_id = ?", "cv", 1).Take(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.MetaJSON != entry.MetaJSON {
		t.Fatal("custom metadata was overwritten")
	}
}

type songCaller struct {
	failRecord bool
	records    int
	texts      []string
}

func (c *songCaller) CallAPI(_ context.Context, req zero.APIRequest) (zero.APIResponse, error) {
	if req.Action != "send_group_msg" {
		return zero.APIResponse{}, fmt.Errorf("unexpected action: %s", req.Action)
	}
	segments, ok := req.Params["message"].(message.Message)
	if !ok {
		return zero.APIResponse{}, fmt.Errorf("unexpected message type: %T", req.Params["message"])
	}
	for _, seg := range segments {
		if seg.Type == "record" {
			c.records++
			if c.failRecord {
				return zero.APIResponse{}, nil
			}
		}
		if seg.Type == "text" {
			c.texts = append(c.texts, seg.Data["text"])
		}
	}
	// Negative OneBot IDs are valid successful sends.
	return zero.APIResponse{Data: gjson.Parse(`{"message_id":-12}`)}, nil
}

func TestGameDeliveryAndAnswer(t *testing.T) {
	db := gameDB(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cache"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cache", "1.mp3"), audioFixture(t), 0600); err != nil {
		t.Fatal(err)
	}
	entry := testTrack(1)
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	old := songs
	songs = newSongService(root, &neteaseClient{}) // cached game must work offline
	t.Cleanup(func() { songs = old })
	caller := &songCaller{failRecord: true}
	zero.APICallers.Store(98765, caller)
	t.Cleanup(func() { zero.APICallers.Delete(98765) })
	ctx := zero.GetBot(98765)
	ctx.Event = &zero.Event{SelfID: 98765, GroupID: 123, UserID: 456, DetailType: "group"}
	startGame(ctx, "cv")
	session, err := database.LoadGuessGameSession(context.Background(), nil, 123)
	if err != nil {
		t.Fatal(err)
	}
	if session.Active || caller.records != 1 {
		t.Fatal("failed audio delivery left an active game")
	}
	if _, err := os.Stat(session.Slice1Path); !os.IsNotExist(err) {
		t.Fatal("failed delivery left slice files")
	}
	caller.failRecord = false
	startGame(ctx, "cv")
	session, err = database.LoadGuessGameSession(context.Background(), nil, 123)
	if err != nil || !session.Active {
		t.Fatalf("start: %+v %v", session, err)
	}
	caller.failRecord = true
	sendHint(ctx)
	session, _ = database.LoadGuessGameSession(context.Background(), nil, 123)
	if session.HintStage != 0 {
		t.Fatal("failed hint consumed a stage")
	}
	caller.failRecord = false
	sendHint(ctx)
	sendHint(ctx)
	session, _ = database.LoadGuessGameSession(context.Background(), nil, 123)
	if session.HintStage != 2 {
		t.Fatal("hints did not advance")
	}
	answerGame(ctx, "测试曲")
	session, _ = database.LoadGuessGameSession(context.Background(), nil, 123)
	if session.Active || session.AnswerCount != 1 || !strings.Contains(caller.texts[len(caller.texts)-1], "答对啦") {
		t.Fatal("answer did not finish the game")
	}
	if _, err := os.Stat(session.Slice2Path); !os.IsNotExist(err) {
		t.Fatal("finished game left slice files")
	}
	startGame(ctx, "cv")
	endGame(ctx)
	if _, err := os.Stat(session.Slice1Path); !os.IsNotExist(err) {
		t.Fatal("abandoned game left slice files")
	}
}
