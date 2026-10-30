package sing

import (
	"context"
	"os"
	"testing"
	"time"
)

// Opt-in: submits one real score synthesis task, without sending any QQ messages.
func TestLiveService(t *testing.T) {
	if os.Getenv("DSAPI_LIVE_TEST") != "1" {
		t.Skip("set DSAPI_LIVE_TEST=1 and DSAPI_KEY to run")
	}
	key := os.Getenv("DSAPI_KEY")
	if key == "" {
		t.Fatal("DSAPI_KEY is required")
	}
	base := os.Getenv("DSAPI_BASE_URL")
	if base == "" {
		base = "https://kirameki-dsapi.colanns.me"
	}
	c := newClient(base, key)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	id := os.Getenv("DSAPI_LIVE_TASK_ID")
	if id == "" {
		s, err := c.search(ctx, "雨爱")
		if err != nil {
			t.Fatal(err)
		}
		id, err = c.submit(ctx, s.ID)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("submitted score task=%s song=%s (%s)", id, s.Name, s.ID)
	}
	if err := c.wait(ctx, id, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	data, err := c.audio(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("score audio received: %d bytes", len(data))
}
