package sing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/colanns/gokohime/internal/config"
	zero "github.com/wdvxdr1123/ZeroBot"
)

func TestFailureRepliesThroughSongRequest(t *testing.T) {
	cases := []struct{ raw, stage, code, want string }{
		{"ValueError: Phonemes missing from HubertFA mixed vocabulary: ['zh/H']", "align", "PHONEME_UNSUPPORTED", "《雨爱》有音素不兼容哦——zh/H 暂时无法处理。"},
		{"ValueError: English word missing from voicebank dictionary: wavyarty", "align", "LYRIC_WORD_NOT_FOUND", "《雨爱》词典缺词哦——找不到「wavyarty」的发音。"},
		{"ValueError: no singable lyric lines parsed from LRC within the audio duration", "select", "LYRICS_MISSING", "《雨爱》没有歌词诶！没有找到可以演唱的歌词。"},
		{"ConnectError: [SSL: UNEXPECTED_EOF_WHILE_READING] secret diagnostic", "fetch", "SOURCE_UNAVAILABLE", "《雨爱》连不上下载源啦！稍后再试试吧。"},
		{"CalledProcessError: Command '['ffprobe', '/private/source.audio']' returned non-zero exit status 1.", "fetch", "AUDIO_READ_FAILED", "《雨爱》音频读取失败！？下载到的文件暂时无法识别。"},
	}
	for _, tc := range cases {
		for _, structured := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/structured=%v", tc.code, structured), func(t *testing.T) {
				result := taskResult{Status: "failed", Stage: tc.stage, Error: tc.raw}
				if structured {
					info := legacyFailure(tc.raw, tc.stage)
					result.ErrorInfo = &info
					result.Error = "raw diagnostics omitted"
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/voices":
						fmt.Fprint(w, `{"voices":[{"id":"vocaloid-luotianyi","name":"洛天依","aliases":["洛"],"available":true}]}`)
					case "/resolve_song":
						fmt.Fprint(w, `{"song":{"id":"1","name":"雨爱"}}`)
					case "/release_task":
						fmt.Fprint(w, `{"task_id":"failed-song"}`)
					case "/query_result":
						json.NewEncoder(w).Encode(result)
					default:
						t.Errorf("unexpected request %s", r.URL.Path)
						http.NotFound(w, r)
					}
				}))
				defer server.Close()
				capture := &caller{sent: make(chan delivery, 2)}
				zero.APICallers.Store(987655, capture)
				defer zero.APICallers.Delete(987655)
				// No song metadata in the failure result: use the resolved title, not the
				// singer selector, artist name, full lyrics, or numeric task ID.
				runSing(destination{987655, 123, 456}, "洛 雨爱 杨丞琳", "score", false, config.SingConfig{BaseURL: server.URL, APIKey: "test"})
				got := receive(t, capture)
				if text := got.segments[0].Data["text"]; text != tc.want {
					t.Fatalf("got %q, want %q", text, tc.want)
				}
			})
		}
	}
}

func TestFailurePreservesSongMetadataAndErrorIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"failed","song":{"name":"另一首"},"error":"sensitive /private/path?key=secret","error_info":{"code":"NEW_UNKNOWN_CODE"}}`)
	}))
	defer server.Close()
	err := newClient(server.URL, "key", "score").wait(context.Background(), "task", time.Millisecond)
	if !errors.Is(err, errGeneration) {
		t.Fatalf("wrong error: %v", err)
	}
	reply, ok := generationReply(fmt.Errorf("task: %w", err), "fallback")
	if !ok || !strings.HasPrefix(reply, "《另一首》翻唱失败了") {
		t.Fatalf("wrong reply: %s", reply)
	}
	for _, text := range []string{reply, err.Error()} {
		if strings.Contains(text, "sensitive") || strings.Contains(text, "secret") || strings.Contains(text, "/private") {
			t.Fatal("leaked diagnostics")
		}
	}
}

func TestFailureFallbackAndSafeTokens(t *testing.T) {
	info := failureInfo{Code: "LYRIC_WORD_NOT_FOUND"}
	info.Context.Word = "/private/path?key=secret"
	reply, _ := generationReply(&generationError{Info: info}, "自填乐谱")
	if reply != "《自填乐谱》词典缺词哦——有些歌词找不到对应的发音。" {
		t.Fatal(reply)
	}
	info.Code = "PHONEME_UNSUPPORTED"
	info.Context.Phonemes = []string{"zh/H", "token\nsecret"}
	reply, _ = generationReply(&generationError{Info: info}, "")
	if reply != "这首歌有音素不兼容哦——zh/H 暂时无法处理。" {
		t.Fatal(reply)
	}
	if legacyFailure("ConnectError: model unavailable", "render_score").Code != "UNKNOWN" {
		t.Fatal("model error misclassified as music source")
	}
	if legacyFailure("no complete lyric lines in the requested time window", "select").Code != "UNKNOWN" {
		t.Fatal("invalid selection misclassified as absent lyrics")
	}
	if _, ok := generationReply(context.DeadlineExceeded, "song"); ok {
		t.Fatal("poll timeout misclassified as synthesis failure")
	}
}
