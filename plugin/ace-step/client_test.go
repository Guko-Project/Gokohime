package acestep

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildAudioURL(t *testing.T) {
	client := newClient("https://colasama--ace-step-api-serve.modal.run/", "")
	filename := "76f85bc5-7679-5350-e866-0b7bb2a7bd3a.mp3"
	want := "https://colasama--ace-step-api-serve.modal.run/v1/audio?path=%2Fapp%2F.cache%2Facestep%2Ftmp%2Fapi_audio%2F" + filename

	tests := []string{
		"/v1/audio?path=%2Fapp%2F.cache%2Facestep%2Ftmp%2Fapi_audio%2F" + filename,
		"/v1/audio?path=/app/.cache/acestep/tmp/api_audio/" + filename,
		"/app/.cache/acestep/tmp/api_audio/" + filename,
		filename,
	}

	for _, tt := range tests {
		got, err := client.buildAudioURL(tt)
		if err != nil {
			t.Fatalf("buildAudioURL(%q) returned error: %v", tt, err)
		}
		if got != want {
			t.Fatalf("buildAudioURL(%q) = %q, want %q", tt, got, want)
		}
	}
}

func TestParseGenerationInputWithExplicitLyrics(t *testing.T) {
	input := parseGenerationInput("日本VOCALOID摇滚 | 我不想上班\n我真的不想上班", 45, 60)
	if input.Prompt != "日本VOCALOID摇滚" {
		t.Fatalf("prompt = %q", input.Prompt)
	}
	if input.Lyrics != "我不想上班\n我真的不想上班" {
		t.Fatalf("lyrics = %q", input.Lyrics)
	}
	if input.VocalLanguage != "zh" {
		t.Fatalf("vocal language = %q", input.VocalLanguage)
	}
	if input.Duration != -1 || input.DurationSet {
		t.Fatalf("duration = %v, durationSet = %v", input.Duration, input.DurationSet)
	}
}

func TestParseGenerationInputAllowsLongDuration(t *testing.T) {
	input := parseGenerationInput("日本VOCALOID摇滚 | 我不想上班 180", 45, 60)
	if input.Duration != 180 || !input.DurationSet {
		t.Fatalf("duration = %v, durationSet = %v", input.Duration, input.DurationSet)
	}
	if input.Lyrics != "我不想上班" {
		t.Fatalf("lyrics = %q", input.Lyrics)
	}
}

func TestBuildSampleQueryRequestsVocals(t *testing.T) {
	got := buildSampleQuery("日本VOCALOID摇滚")
	if !strings.Contains(got, "Generate sung vocals") {
		t.Fatalf("sample query = %q", got)
	}
}

func TestApplySampleDataUsesFormattedFields(t *testing.T) {
	input := generationInput{
		Prompt:        "日本VOCALOID摇滚",
		Lyrics:        "我不想上班 我真的不想上班",
		VocalLanguage: "zh",
		Duration:      -1,
	}
	sample := &sampleData{
		Caption:        "formatted caption",
		Lyrics:         "[Verse 1]\n我不想上班\n我真的不想上班",
		BPM:            176,
		KeyScale:       "D major",
		TimeSignature2: "4",
		Duration:       55,
		VocalLanguage:  "zh",
	}

	applySampleData(&input, sample, true)

	if input.Prompt != "formatted caption" || input.Lyrics != sample.Lyrics {
		t.Fatalf("input = %+v", input)
	}
	if input.BPM != 176 || input.Keyscale != "D major" || input.TimeSignature != "4" {
		t.Fatalf("metadata = %+v", input)
	}
	if input.Duration != 55 {
		t.Fatalf("duration = %v", input.Duration)
	}
}

func TestSampleModeReleasePayloadDoesNotForceInstrumental(t *testing.T) {
	input := generationInput{
		Prompt:        "生成一段中文VOCALOID歌曲，需要有歌词",
		Lyrics:        "[Verse]\n我不想上班",
		VocalLanguage: "zh",
		Duration:      120,
	}
	payload := releaseTaskPayload(input)
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, `"sample_mode":false`) {
		t.Fatalf("payload should explicitly disable sample mode: %s", body)
	}
	if strings.Contains(body, `"sample_query"`) {
		t.Fatalf("payload should not include sample query: %s", body)
	}
	if !strings.Contains(body, `"thinking":false`) {
		t.Fatalf("payload should disable thinking: %s", body)
	}
	for _, field := range []string{`"use_format":false`, `"use_cot_caption":false`, `"use_cot_language":false`, `"use_cot_metas":false`} {
		if !strings.Contains(body, field) {
			t.Fatalf("payload should disable ACE-Step built-in LM/COT field %s: %s", field, body)
		}
	}
	if !strings.Contains(body, `"audio_duration":120`) {
		t.Fatalf("payload should include audio_duration: %s", body)
	}
}

func TestResolveDurationPriorityAndCap(t *testing.T) {
	formatted := &deepSeekFormatResult{Duration: 90}
	input := generationInput{Duration: 240, DurationSet: true}
	if got := resolveDuration(input, formatted, 30); got != 180 {
		t.Fatalf("explicit duration should win and cap to 180, got %v", got)
	}

	input = generationInput{}
	if got := resolveDuration(input, formatted, 30); got != 90 {
		t.Fatalf("deepseek duration should be used when explicit duration absent, got %v", got)
	}

	formatted.Duration = 240
	if got := resolveDuration(input, formatted, 30); got != 180 {
		t.Fatalf("deepseek duration should cap to 180, got %v", got)
	}
}

func TestBuildDeepSeekUserContentOmitsExplicitDurationValue(t *testing.T) {
	input := generationInput{Prompt: "日本VOCALOID摇滚", Lyrics: "我不想上班", VocalLanguage: "zh", Duration: 240, DurationSet: true}
	content, err := buildDeepSeekUserContent(input)
	if err != nil {
		t.Fatalf("build content: %v", err)
	}
	if strings.Contains(content, "240") {
		t.Fatalf("deepseek user content should not include explicit duration value: %s", content)
	}
	if !strings.Contains(content, `"has_explicit_duration":true`) {
		t.Fatalf("deepseek user content should include explicit duration hint: %s", content)
	}
}

func TestParseDeepSeekFormatResultValidatesSchema(t *testing.T) {
	valid := `{"caption":"Japanese VOCALOID rock, energetic guitars","lyrics":"[Verse]\n我不想上班","bpm":176,"key_scale":"D major","time_signature":"4/4","vocal_language":"zh","instrumental":false,"duration":120}`
	parsed, err := parseDeepSeekFormatResult(valid)
	if err != nil {
		t.Fatalf("valid result rejected: %v", err)
	}
	if parsed.Caption == "" || parsed.Duration != 120 {
		t.Fatalf("unexpected parsed result: %+v", parsed)
	}

	invalid := `{"lyrics":"[Verse]\n我不想上班","instrumental":false,"duration":120}`
	if _, err := parseDeepSeekFormatResult(invalid); err == nil {
		t.Fatalf("missing caption should be rejected")
	}

	invalid = `{"caption":"x","lyrics":"[Verse]","bpm":400,"time_signature":"4/4","vocal_language":"zh","instrumental":false}`
	if _, err := parseDeepSeekFormatResult(invalid); err == nil {
		t.Fatalf("out of range bpm should be rejected")
	}
}
