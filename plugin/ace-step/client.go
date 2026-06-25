package acestep

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	log "github.com/colanns/gokohime/internal/log"
)

type aceClient struct {
	baseURL         string
	apiKey          string
	httpClient      *http.Client
	useDeepSeekLM   bool
	deepSeekAPIKey  string
	deepSeekBaseURL string
	deepSeekModel   string
}

const (
	lmRequestTimeout = 180 * time.Second
	lmRetryCount     = 2
)

type releaseTaskReq struct {
	Prompt         string  `json:"prompt"`
	SampleMode     bool    `json:"sample_mode"`
	SampleQuery    string  `json:"sample_query,omitempty"`
	Lyrics         string  `json:"lyrics,omitempty"`
	VocalLanguage  string  `json:"vocal_language,omitempty"`
	Instrumental   *bool   `json:"instrumental,omitempty"`
	BPM            int     `json:"bpm,omitempty"`
	KeyScale       string  `json:"key_scale,omitempty"`
	TimeSignature  string  `json:"time_signature,omitempty"`
	AudioDuration  float64 `json:"audio_duration,omitempty"`
	Thinking       bool    `json:"thinking"`
	UseFormat      bool    `json:"use_format"`
	UseCOTCaption  bool    `json:"use_cot_caption"`
	UseCOTLanguage bool    `json:"use_cot_language"`
	UseCOTMetas    bool    `json:"use_cot_metas"`
	InferenceSteps int     `json:"inference_steps"`
	AudioFormat    string  `json:"audio_format"`
	BatchSize      int     `json:"batch_size"`
}

type createSampleReq struct {
	SampleQuery string `json:"sample_query"`
}

type sampleData struct {
	Caption        string  `json:"caption"`
	Lyrics         string  `json:"lyrics"`
	BPM            int     `json:"bpm"`
	Keyscale       string  `json:"keyscale"`
	KeyScale       string  `json:"key_scale"`
	Duration       float64 `json:"duration"`
	TimeSignature  string  `json:"timesignature"`
	TimeSignature2 string  `json:"time_signature"`
	VocalLanguage  string  `json:"vocal_language"`
}

type formatInputReq struct {
	Prompt        string `json:"prompt"`
	Lyrics        string `json:"lyrics"`
	VocalLanguage string `json:"vocal_language,omitempty"`
}

type apiResponse struct {
	Data      json.RawMessage `json:"data"`
	Code      int             `json:"code"`
	Error     *string         `json:"error"`
	Timestamp int64           `json:"timestamp"`
}

func (s *sampleData) normalizedKeyscale() string {
	if strings.TrimSpace(s.Keyscale) != "" {
		return strings.TrimSpace(s.Keyscale)
	}
	return strings.TrimSpace(s.KeyScale)
}

func (s *sampleData) normalizedTimeSignature() string {
	if strings.TrimSpace(s.TimeSignature) != "" {
		return strings.TrimSpace(s.TimeSignature)
	}
	return strings.TrimSpace(s.TimeSignature2)
}

type taskData struct {
	TaskID string `json:"task_id"`
}

// Actual response format from query_result
type queryResultItem struct {
	TaskID       string `json:"task_id"`
	Result       string `json:"result"` // JSON-stringified array
	Status       int    `json:"status"` // 0=processing, 1=completed
	ProgressText string `json:"progress_text"`
}

// Parsed from the "result" string
type resultEntry struct {
	File   string `json:"file"`
	Stage  string `json:"stage"` // "succeeded"
	Status int    `json:"status"`
}

func newClient(baseURL, apiKey string) *aceClient {
	return &aceClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func newClientWithConfig(baseURL, apiKey string, useDeepSeekLM bool, deepSeekAPIKey, deepSeekBaseURL, deepSeekModel string) *aceClient {
	c := newClient(baseURL, apiKey)
	c.useDeepSeekLM = useDeepSeekLM
	c.deepSeekAPIKey = deepSeekAPIKey
	c.deepSeekBaseURL = strings.TrimRight(deepSeekBaseURL, "/")
	c.deepSeekModel = deepSeekModel
	return c
}

func (c *aceClient) doRequest(method, path string, body io.Reader) (*http.Response, error) {
	url := c.baseURL + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

func (c *aceClient) doLMRequest(method, path string, data []byte) (*http.Response, error) {
	client := &http.Client{Timeout: lmRequestTimeout}
	var lastErr error

	for attempt := 0; attempt <= lmRetryCount; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt*5) * time.Second
			log.Warnf("[ace-step] LM request retrying after error: %v (attempt %d/%d, wait %v)", lastErr, attempt+1, lmRetryCount+1, backoff)
			time.Sleep(backoff)
		}

		url := c.baseURL + path
		req, err := http.NewRequest(method, url, strings.NewReader(string(data)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}

	return nil, lastErr
}

// createSample asks the 5Hz LM to expand a natural-language request into
// caption, lyrics, and metadata for generation.
func (c *aceClient) createSample(query string) (*sampleData, error) {
	payload := createSampleReq{SampleQuery: query}
	data, _ := json.Marshal(payload)
	log.Infof("[ace-step] create_sample API payload: %s", compactJSONForLog(data))

	resp, err := c.doLMRequest("POST", "/v1/create_sample", data)
	if err != nil {
		return nil, fmt.Errorf("create sample request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read create sample response: %w", err)
	}

	var apiResp apiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("parse create sample response: %w (body: %s)", err, string(respBody))
	}
	if apiResp.Code != 200 {
		errMsg := "unknown"
		if apiResp.Error != nil {
			errMsg = *apiResp.Error
		}
		return nil, fmt.Errorf("create sample API error %d: %s", apiResp.Code, errMsg)
	}

	var sample sampleData
	if err := json.Unmarshal(apiResp.Data, &sample); err != nil {
		return nil, fmt.Errorf("parse sample data: %w", err)
	}
	return &sample, nil
}

func (c *aceClient) formatInput(prompt, lyrics, vocalLanguage string) (*sampleData, error) {
	payload := formatInputReq{
		Prompt:        prompt,
		Lyrics:        lyrics,
		VocalLanguage: vocalLanguage,
	}
	data, _ := json.Marshal(payload)
	log.Infof("[ace-step] format_input API payload: %s", compactJSONForLog(data))

	resp, err := c.doLMRequest("POST", "/format_input", data)
	if err != nil {
		return nil, fmt.Errorf("format input request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read format input response: %w", err)
	}

	var apiResp apiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("parse format input response: %w (body: %s)", err, string(respBody))
	}
	if apiResp.Code != 200 {
		errMsg := "unknown"
		if apiResp.Error != nil {
			errMsg = *apiResp.Error
		}
		return nil, fmt.Errorf("format input API error %d: %s", apiResp.Code, errMsg)
	}

	var sample sampleData
	if err := json.Unmarshal(apiResp.Data, &sample); err != nil {
		return nil, fmt.Errorf("parse format input data: %w", err)
	}
	return &sample, nil
}

// submitTask submits a music generation task, returns task_id.
func (c *aceClient) submitTask(input generationInput) (string, error) {
	payload := releaseTaskPayload(input)
	data, _ := json.Marshal(payload)
	log.Infof("[ace-step] release_task API payload: %s", compactJSONForLog(data))

	resp, err := c.doRequest("POST", "/release_task", strings.NewReader(string(data)))
	if err != nil {
		return "", fmt.Errorf("submit request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var apiResp apiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("parse response: %w (body: %s)", err, string(respBody))
	}
	if apiResp.Code != 200 {
		errMsg := "unknown"
		if apiResp.Error != nil {
			errMsg = *apiResp.Error
		}
		return "", fmt.Errorf("API error %d: %s", apiResp.Code, errMsg)
	}

	var td taskData
	if err := json.Unmarshal(apiResp.Data, &td); err != nil {
		return "", fmt.Errorf("parse task data: %w", err)
	}
	return td.TaskID, nil
}

func releaseTaskPayload(input generationInput) releaseTaskReq {
	prompt := input.Prompt
	lyrics := input.Lyrics
	var instrumental *bool
	if !input.SampleMode {
		isInstrumental := isInstrumentalLyrics(lyrics)
		instrumental = &isInstrumental
	}
	return releaseTaskReq{
		Prompt:         prompt,
		SampleMode:     input.SampleMode,
		SampleQuery:    input.SampleQuery,
		Lyrics:         lyrics,
		VocalLanguage:  input.VocalLanguage,
		Instrumental:   instrumental,
		BPM:            input.BPM,
		KeyScale:       input.Keyscale,
		TimeSignature:  input.TimeSignature,
		AudioDuration:  input.Duration,
		Thinking:       false,
		UseFormat:      false,
		UseCOTCaption:  false,
		UseCOTLanguage: false,
		UseCOTMetas:    false,
		InferenceSteps: 10,
		AudioFormat:    "mp3",
		BatchSize:      1,
	}
}

func isInstrumentalLyrics(lyrics string) bool {
	normalized := strings.ToLower(strings.TrimSpace(lyrics))
	return normalized == "" || normalized == "[instrumental]"
}

func compactJSONForLog(data []byte) string {
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return string(data)
	}
	formatted, err := json.Marshal(out)
	if err != nil {
		return string(data)
	}
	return string(formatted)
}

func resolveDuration(input generationInput, formatted *deepSeekFormatResult, defaultDuration int) float64 {
	duration := input.Duration
	if !input.DurationSet {
		if formatted != nil && formatted.Duration > 0 {
			duration = formatted.Duration
		} else if duration <= 0 {
			duration = float64(defaultDuration)
		}
	}
	if duration <= 0 {
		duration = float64(defaultDuration)
	}
	if duration > 180 {
		return 180
	}
	return duration
}

// pollResult polls until task is completed or failed. Returns audio file path on success.
func (c *aceClient) pollResult(taskID string, pollInterval, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Build request body: task_id_list is a JSON-stringified array
		listJSON, _ := json.Marshal([]string{taskID})
		reqBody := fmt.Sprintf(`{"task_id_list": %s}`, strconv.Quote(string(listJSON)))
		log.Infof("[ace-step] query_result API payload: %s", reqBody)

		resp, err := c.doRequest("POST", "/query_result", strings.NewReader(reqBody))
		if err != nil {
			log.Warnf("[ace-step] poll request error: %v", err)
			time.Sleep(pollInterval)
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var apiResp apiResponse
		if err := json.Unmarshal(respBody, &apiResp); err != nil {
			log.Warnf("[ace-step] poll parse error: %v", err)
			time.Sleep(pollInterval)
			continue
		}

		// data is an array of queryResultItem
		var items []queryResultItem
		if err := json.Unmarshal(apiResp.Data, &items); err != nil {
			log.Warnf("[ace-step] poll data parse error: %v (raw: %s)", err, string(apiResp.Data))
			time.Sleep(pollInterval)
			continue
		}

		if len(items) == 0 {
			time.Sleep(pollInterval)
			continue
		}

		item := items[0]
		// status: 0 = processing, 1 = completed
		if item.Status == 1 {
			// Parse the result string (JSON-stringified array)
			var entries []resultEntry
			if err := json.Unmarshal([]byte(item.Result), &entries); err != nil {
				return "", fmt.Errorf("parse result entries: %w (raw: %s)", err, item.Result)
			}
			if len(entries) == 0 {
				return "", fmt.Errorf("completed but no result entries")
			}
			// Return the first successful entry's file URL
			for _, e := range entries {
				if e.File != "" {
					return e.File, nil
				}
			}
			return "", fmt.Errorf("completed but no audio file in results")
		}

		time.Sleep(pollInterval)
	}

	return "", fmt.Errorf("timeout waiting for result (%v)", timeout)
}

const aceAudioDir = "/app/.cache/acestep/tmp/api_audio"

// buildAudioURL builds the ACE-Step audio download URL while preserving the
// encoded query path expected by the upstream service.
func (c *aceClient) buildAudioURL(fileField string) (string, error) {
	filename := extractAudioFilename(fileField)
	if filename == "" {
		return "", fmt.Errorf("empty audio filename from result: %q", fileField)
	}

	audioPath := path.Join(aceAudioDir, filename)
	parsedBaseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base URL: %w", err)
	}
	parsedBaseURL.Path = strings.TrimRight(parsedBaseURL.Path, "/") + "/v1/audio"
	parsedBaseURL.RawQuery = "path=" + url.QueryEscape(audioPath)
	return parsedBaseURL.String(), nil
}

func extractAudioFilename(fileField string) string {
	if parsed, err := url.Parse(fileField); err == nil {
		if pathValue := parsed.Query().Get("path"); pathValue != "" {
			return path.Base(pathValue)
		}
	}

	decoded, err := url.QueryUnescape(fileField)
	if err != nil {
		decoded = fileField
	}
	return path.Base(decoded)
}

// downloadAudio downloads audio to a local file.
func (c *aceClient) downloadAudio(fileField, localPath string) error {
	downloadURL, err := c.buildAudioURL(fileField)
	if err != nil {
		return err
	}

	log.Infof("[ace-step] downloading from: %s", downloadURL)

	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	// Force HTTP/1.1 to prevent HTTP/2 from normalizing %2F in query params
	client := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper),
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download failed: HTTP %d, body: %s", resp.StatusCode, string(body))
	}

	f, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}
