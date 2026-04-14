package imageutil

import (
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
)

var (
	client = &http.Client{Timeout: 30 * time.Second}
	// Semaphore to limit concurrent image processing
	sem = make(chan struct{}, 4)
)

// DownloadAndCompress downloads an image from URL, resizes and compresses it,
// returns a base64 data URL suitable for LLM input.
func DownloadAndCompress(url string, maxSize int, quality int) (string, error) {
	sem <- struct{}{}
	defer func() { <-sem }()

	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download image: status %d", resp.StatusCode)
	}

	return compressFromReader(resp.Body, maxSize, quality)
}

func compressFromReader(r io.Reader, maxSize int, quality int) (string, error) {
	src, _, err := image.Decode(r)
	if err != nil {
		return "", fmt.Errorf("decode image: %w", err)
	}

	// Resize if needed
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if maxSize > 0 && (w > maxSize || h > maxSize) {
		if w > h {
			h = h * maxSize / w
			w = maxSize
		} else {
			w = w * maxSize / h
			h = maxSize
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
		src = dst
	}

	// Encode as JPEG
	var buf strings.Builder
	b64w := base64.NewEncoder(base64.StdEncoding, &buf)
	if err := jpeg.Encode(b64w, src, &jpeg.Options{Quality: quality}); err != nil {
		return "", fmt.Errorf("encode jpeg: %w", err)
	}
	b64w.Close()

	return "data:image/jpeg;base64," + buf.String(), nil
}

// ExtractImageURLs extracts image URLs from a QQ message segments.
func ExtractImageURLs(messageSegments []map[string]string) []string {
	var urls []string
	for _, seg := range messageSegments {
		if seg["type"] == "image" && seg["url"] != "" {
			urls = append(urls, seg["url"])
		}
	}
	return urls
}

// Pool for reusing buffers
var bufPool = sync.Pool{
	New: func() interface{} {
		return new(strings.Builder)
	},
}
