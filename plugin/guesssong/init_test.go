package guesssong

import (
	"testing"

	zero "github.com/wdvxdr1123/ZeroBot"
)

func TestGuessSessionIdentity(t *testing.T) {
	groupCtx := &zero.Ctx{Event: &zero.Event{GroupID: 123, UserID: 456}}
	if got := guessSessionID(groupCtx); got != 123 {
		t.Fatalf("guessSessionID group = %d, want 123", got)
	}
	if got := guessSessionDir(groupCtx); got != "group_123" {
		t.Fatalf("guessSessionDir group = %q", got)
	}

	privateCtx := &zero.Ctx{Event: &zero.Event{UserID: 456}}
	if got := guessSessionID(privateCtx); got != -456 {
		t.Fatalf("guessSessionID private = %d, want -456", got)
	}
	if got := guessSessionDir(privateCtx); got != "private_456" {
		t.Fatalf("guessSessionDir private = %q", got)
	}
}

func TestPrepareSongFailureText(t *testing.T) {
	cases := map[string]string{
		"ffmpeg not found: exec":        "准备题目失败，请先安装 ffmpeg 后再试。",
		"ffmpeg: exit status 1":         "准备题目失败，ffmpeg 切片失败，请检查音频文件和 ffmpeg 是否可用。",
		"download audio status 404":     "准备题目失败，歌曲资源暂时不可用，请稍后再试。",
		"song 1 not found":              "准备题目失败，歌曲资源暂时不可用，请稍后再试。",
		"some network timeout happened": "准备题目失败，请确认网络可用且系统已安装 ffmpeg。",
	}

	for input, want := range cases {
		if got := prepareSongFailureText(assertErr(input)); got != want {
			t.Fatalf("prepareSongFailureText(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestChooseSliceOffsets(t *testing.T) {
	start1, start2 := chooseSliceOffsets(5000)
	if start1 != 0 || start2 != 0 {
		t.Fatalf("short song should return zero offsets, got %d %d", start1, start2)
	}

	start1, start2 = chooseSliceOffsets(120000)
	if start1 < 0 || start2 < sliceLengthSec {
		t.Fatalf("unexpected offsets: %d %d", start1, start2)
	}
	if start2 < start1 {
		t.Fatalf("expected second slice not to start before first slice: %d %d", start1, start2)
	}
}

func assertErr(msg string) error {
	return testErr(msg)
}

type testErr string

func (e testErr) Error() string {
	return string(e)
}
