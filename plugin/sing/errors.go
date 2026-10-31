package sing

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type failureInfo struct {
	Code    string `json:"code"`
	Context struct {
		Word     string   `json:"word"`
		Phonemes []string `json:"phonemes"`
	} `json:"context"`
}

type generationError struct {
	Info     failureInfo
	SongName string
}

// Keep raw server diagnostics out of chat messages and client error strings.
func (e *generationError) Error() string { return "synthesis failed: " + knownCode(e.Info.Code) }
func (e *generationError) Unwrap() error { return errGeneration }

func knownCode(code string) string {
	switch code {
	case "PHONEME_UNSUPPORTED", "LYRIC_WORD_NOT_FOUND", "LYRICS_MISSING", "SOURCE_UNAVAILABLE", "AUDIO_READ_FAILED":
		return code
	default:
		return "UNKNOWN"
	}
}

var missingPhones = regexp.MustCompile(`vocabulary: \[([^\]\n]*)\]`)
var safePhone = regexp.MustCompile(`^[A-Za-z0-9_/#]+$`)
var safeWord = regexp.MustCompile(`^[\p{L}\p{N}_'-]+$`)

// Old servers and persisted jobs have only an error string. Match known failures
// narrowly; never forward the complete string (which can contain local secrets).
func legacyFailure(raw, stage string) failureInfo {
	info := failureInfo{Code: "UNKNOWN"}
	switch {
	case strings.Contains(raw, "Phonemes missing from HubertFA "):
		info.Code = "PHONEME_UNSUPPORTED"
		if match := missingPhones.FindStringSubmatch(raw); len(match) == 2 {
			for _, phone := range strings.Split(match[1], ",") {
				phone = strings.Trim(strings.TrimSpace(phone), "'\"")
				if safePhone.MatchString(phone) && len(phone) <= 48 {
					info.Context.Phonemes = append(info.Context.Phonemes, phone)
				}
			}
		}
	case strings.Contains(raw, "word missing from voicebank dictionary:"):
		info.Code = "LYRIC_WORD_NOT_FOUND"
		_, word, _ := strings.Cut(raw, "word missing from voicebank dictionary:")
		info.Context.Word = strings.TrimSpace(strings.SplitN(word, "\n", 2)[0])
	case strings.Contains(raw, "no singable lyric lines parsed from LRC"), strings.Contains(raw, "song has no LRC lyrics"):
		info.Code = "LYRICS_MISSING"
	case stage == "fetch" && strings.Contains(raw, "ffprobe"):
		info.Code = "AUDIO_READ_FAILED"
	case stage == "fetch":
		for _, marker := range []string{"ConnectError:", "ConnectTimeout:", "ReadTimeout:", "ReadError:", "RemoteProtocolError:", "HTTPStatusError:", "UNEXPECTED_EOF_WHILE_READING"} {
			if strings.Contains(raw, marker) {
				info.Code = "SOURCE_UNAVAILABLE"
				break
			}
		}
	}
	return info
}

func songLabel(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "这首歌"
	}
	chars := []rune(name)
	if len(chars) > 80 {
		name = string(chars[:80]) + "…"
	}
	return "《" + name + "》"
}

func generationReply(err error, fallback string) (string, bool) {
	var failure *generationError
	if !errors.As(err, &failure) {
		return "", false
	}
	name := failure.SongName
	if strings.TrimSpace(name) == "" {
		name = fallback
	}
	prefix := songLabel(name)
	switch failure.Info.Code {
	case "PHONEME_UNSUPPORTED":
		phones := []string{}
		for _, p := range failure.Info.Context.Phonemes {
			if safePhone.MatchString(p) && len(p) <= 48 {
				phones = append(phones, p)
			}
			if len(phones) == 4 {
				break
			}
		}
		if len(phones) > 0 {
			return prefix + "有音素不兼容哦——" + strings.Join(phones, "、") + " 暂时无法处理。", true
		}
		return prefix + "有音素不兼容哦——当前音源的部分发音暂时无法处理。", true
	case "LYRIC_WORD_NOT_FOUND":
		word := failure.Info.Context.Word
		if safeWord.MatchString(word) && len([]rune(word)) <= 64 {
			return prefix + fmt.Sprintf("词典缺词哦——找不到「%s」的发音。", word), true
		}
		return prefix + "词典缺词哦——有些歌词找不到对应的发音。", true
	case "LYRICS_MISSING":
		return prefix + "没有歌词诶！没有找到可以演唱的歌词。", true
	case "SOURCE_UNAVAILABLE":
		return prefix + "连不上下载源啦！稍后再试试吧。", true
	case "AUDIO_READ_FAILED":
		return prefix + "音频读取失败！？下载到的文件暂时无法识别。", true
	default:
		return prefix + "翻唱失败了……暂时没能完成合成，请稍后再试试。", true
	}
}
