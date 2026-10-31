package sing

import (
	"errors"
	"testing"

	zero "github.com/wdvxdr1123/ZeroBot"
)

func TestTimeRange(t *testing.T) {
	for _, tc := range []struct {
		input, query string
		start, end   float64
	}{
		{"初 歌名 0-120", "初 歌名", 0, 120},
		{"洛 歌名 12.5-90", "洛 歌名", 12.5, 90},
		{"心 第一句\n第二句\n第三句　0-120 ", "心 第一句\n第二句\n第三句", 0, 120},
	} {
		q, start, end, err := parseTimeRange(tc.input)
		if err != nil || q != tc.query || start == nil || end == nil || *start != tc.start || *end != tc.end {
			t.Errorf("parse %q: %q %v %v %v", tc.input, q, start, end, err)
		}
	}
	for _, q := range []string{"歌名", "初 歌名", "第一句\n第二句\n第三句", "歌曲0-120", "歌名 2002", "a-b"} {
		got, start, end, err := parseTimeRange(q)
		if err != nil || got != q || start != nil || end != nil {
			t.Errorf("changed query %q", q)
		}
	}
	for _, q := range []string{"歌名 120-0", "歌名 0-0", "歌名 -1-120", "歌名 0-1e999"} {
		if _, _, _, err := parseTimeRange(q); !errors.Is(err, errTimeRange) {
			t.Errorf("accepted %q: %v", q, err)
		}
	}
	if _, _, _, err := parseTimeRange("0-120"); !errors.Is(err, errEmptyQuery) {
		t.Fatal(err)
	}
}

func TestSongWithoutArgumentsShowsHelp(t *testing.T) {
	for _, command := range []string{"song", "bsong", "bmsong"} {
		c := &caller{sent: make(chan delivery, 2)}
		zero.APICallers.Store(987653, c)
		defer zero.APICallers.Delete(987653)
		ctx := zero.GetBot(987653)
		ctx.Event = &zero.Event{SelfID: 987653, UserID: 456}
		ctx.State = zero.State{"command": command, "args": " \n "}
		handleSing(ctx)
		if got := receive(t, c).segments[0].Data["text"]; got != singHelp {
			t.Fatalf("wrong help: %s", got)
		}
		if len(c.sent) != 0 {
			t.Fatal("help submitted a job")
		}
	}
}
