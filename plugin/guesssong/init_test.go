package guesssong

import (
	"context"
	"fmt"
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
	cases := map[error]string{
		errAudioTools:            "准备题目失败，请先安装 ffmpeg 和 ffprobe 后再试。",
		errSongDownload:          "下载歌曲失败惹！可以联系 Colanns 看看怎么回事——",
		errSongUnavailable:       "下载歌曲失败惹！可以联系 Colanns 看看怎么回事——",
		errRateLimited:           "下载歌曲失败惹！可以联系 Colanns 看看怎么回事——",
		context.DeadlineExceeded: "下载歌曲失败惹！可以联系 Colanns 看看怎么回事——",
	}

	for input, want := range cases {
		if got := prepareSongFailureText(fmt.Errorf("wrapped: %w", input)); got != want {
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

func TestSliceOffsetsStayWithinAudio(t *testing.T) {
	for seconds := 12; seconds < 1000; seconds++ {
		for i := 0; i < 10; i++ {
			a, b := chooseSliceOffsets(seconds * 1000)
			if a < 0 || a+sliceLengthSec > b || b+sliceLengthSec > seconds {
				t.Fatalf("duration=%d offsets=%d,%d", seconds, a, b)
			}
		}
	}
}
