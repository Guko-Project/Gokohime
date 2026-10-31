package sing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testCatalog() voiceCatalog {
	return voiceCatalog{Voices: []voice{
		{ID: defaultVoiceID, Name: "洛天依", Aliases: []string{"洛天依", "洛", "天依"}, Available: true},
		{ID: "miku", Name: "初音未来", Aliases: []string{"初音", "初日", "Miku"}, Available: true},
		{ID: "miku-chn", Name: "初音未来（中文版）", Aliases: []string{"初音中文", "中文初音", "初中", "初"}, Available: true},
		{ID: "xinhua", Name: "心华", Aliases: []string{"心华", "心"}, Available: true},
	}}
}

func TestVoicePrefixesAndFallback(t *testing.T) {
	for _, tc := range []struct{ input, id, query string }{
		{"洛 雨爱 杨丞琳", defaultVoiceID, "雨爱 杨丞琳"},
		{"洛天依 雨爱", defaultVoiceID, "雨爱"},
		{"初 雨爱", "miku-chn", "雨爱"},
		{"初日 你好", "miku", "你好"},
		{"初中 雨爱", "miku-chn", "雨爱"},
		{"初音中文 雨爱", "miku-chn", "雨爱"},
		{"中文初音 雨爱", "miku-chn", "雨爱"},
		{"心\t第一句\n第二句\n第三句", "xinhua", "第一句\n第二句\n第三句"},
		{"初音 你好", "miku", "你好"},
		{"MIKU　hello world", "miku", "hello world"},
		{"周杰伦 晴天", defaultVoiceID, "周杰伦 晴天"},
		{"心愿", defaultVoiceID, "心愿"},
		{"洛天依的消失", defaultVoiceID, "洛天依的消失"},
		{"未知 第一行\n第二行", defaultVoiceID, "未知 第一行\n第二行"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			v, q, err := selectVoice(tc.input, testCatalog())
			if err != nil || v.ID != tc.id || q != tc.query {
				t.Fatalf("got %s %q %v", v.ID, q, err)
			}
		})
	}
	if _, _, err := selectVoice("心", testCatalog()); !errors.Is(err, errEmptyQuery) {
		t.Fatal(err)
	}
	catalog := testCatalog()
	catalog.Voices[3].Available = false
	if _, _, err := selectVoice("心 你好", catalog); !errors.Is(err, errVoiceUnavailable) {
		t.Fatal("unavailable voice must not fall back")
	}
}

func TestVoiceCatalogCacheAndSelectedID(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Error("missing auth")
		}
		if r.URL.Path != "/voices" {
			t.Error(r.URL.Path)
		}
		calls++
		fmt.Fprint(w, `{"voices":[{"id":"vocaloid-luotianyi","name":"洛天依","aliases":["洛"],"available":true}]}`)
	}))
	defer server.Close()
	c := newClient(server.URL, "key", "score")
	for i := 0; i < 2; i++ {
		catalog, err := c.voices(context.Background())
		if err != nil || len(catalog.Voices) != 1 {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("catalog calls: %d", calls)
	}
}

func TestOrderedAudioParts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/query_result":
			fmt.Fprint(w, `{"status":"done","outputs":{"score":"/full","score_part_0001":"https://untrusted.invalid/1","score_part_0000":"/0","mimic_part_0000":"/wrong","score_part_bad":"/bad"}}`)
		case "/v1/audio":
			fmt.Fprint(w, "ID3"+r.URL.Query().Get("kind"))
		default:
			t.Error("unexpected path")
		}
	}))
	defer server.Close()
	c := newClient(server.URL, "key", "score")
	if err := c.wait(context.Background(), "task", time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(c.parts) != 2 || c.parts[0] != "score_part_0000" || c.parts[1] != "score_part_0001" {
		t.Fatal(c.parts)
	}
	for _, part := range c.parts {
		body, err := c.audioKind(context.Background(), "task", part)
		if err != nil || string(body) != "ID3"+part {
			t.Fatal(err)
		}
	}
}
