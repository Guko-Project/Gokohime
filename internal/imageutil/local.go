package imageutil

import (
	"fmt"
	"os"

	"github.com/wdvxdr1123/ZeroBot/message"
)

// LocalImageSegment loads a local image file and converts it to a base64 image
// segment so adapters that reject file:// URIs can still send it.
func LocalImageSegment(path string) (message.Segment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return message.Segment{}, fmt.Errorf("read local image: %w", err)
	}
	return message.ImageBytes(data), nil
}
