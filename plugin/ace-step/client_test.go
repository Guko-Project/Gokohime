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
		Prompt:      "生成一段中文VOCALOID歌曲，需要有歌词",
		SampleMode:  true,
		SampleQuery: "生成一段中文VOCALOID歌曲，需要有歌词\nGenerate sung vocals with complete lyrics. Do not make this instrumental.",
	}
	payload := releaseTaskPayload(input)
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, `"sample_mode":true`) || !strings.Contains(body, `"sample_query"`) {
		t.Fatalf("payload missing sample mode fields: %s", body)
	}
	if strings.Contains(body, `"lyrics"`) || strings.Contains(body, `"instrumental"`) {
		t.Fatalf("sample mode payload should not force lyrics/instrumental: %s", body)
	}
}
