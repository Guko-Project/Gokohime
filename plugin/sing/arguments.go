package sing

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const singHelp = `唱歌命令帮助
.song — 显示本帮助；带参数同 .sing
.sing / .ssing [音源] 歌名 [原唱] 或歌词 — score 翻唱
.msing [音源] 歌名 [原唱] 或歌词 — mimic 翻唱
.voices — 查看完整歌手表及别名
.help — 查看全部功能

默认洛天依；音源未命中时按原查询搜歌。
中文初音：初 / 初中；日语初音：初日。
末尾可加 起始秒-结束秒，不受 32/40 秒上限限制：
.sing 初 雨爱 杨丞琳 0-120
.msing 洛 歌名 12.5-90
不指定时间时，三句及以上歌词完整匹配后演唱完整范围，不受短片段长度限制。`

var errTimeRange = errors.New("invalid time range")

const secondsPattern = `[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?`

var timeRangePattern = regexp.MustCompile(`^(` + secondsPattern + `)-(` + secondsPattern + `)$`)

// Only consume a standalone final token, preserving all internal lyric newlines.
func parseTimeRange(query string) (string, *float64, *float64, error) {
	query = strings.TrimSpace(query)
	i := strings.LastIndexFunc(query, unicode.IsSpace)
	token := query
	if i >= 0 {
		token = strings.TrimSpace(query[i:])
	}
	parts := timeRangePattern.FindStringSubmatch(token)
	if parts == nil {
		return query, nil, nil, nil
	}
	start, startErr := strconv.ParseFloat(parts[1], 64)
	end, endErr := strconv.ParseFloat(parts[2], 64)
	if startErr != nil || endErr != nil || math.IsInf(end-start, 0) || start < 0 || end <= start {
		return "", nil, nil, errTimeRange
	}
	if i < 0 || strings.TrimSpace(query[:i]) == "" {
		return "", nil, nil, errEmptyQuery
	}
	return strings.TrimSpace(query[:i]), &start, &end, nil
}
