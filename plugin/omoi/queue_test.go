package omoi

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestQueuedStreamHeartbeatsKeepIdleWatchdogAlive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	idle := time.AfterFunc(100*time.Millisecond, cancel)
	defer idle.Stop()
	go func() {
		defer writer.Close()
		for i := 0; i < 8; i++ {
			_, _ = io.WriteString(writer, "event: ping\ndata: {}\n\n")
			time.Sleep(25 * time.Millisecond)
		}
		_, _ = io.WriteString(writer, "event: queue\ndata: {\"status\":\"running\"}\n\nevent: delta\ndata: {\"content\":\"finished\"}\n\nevent: done\ndata: {}\n\n")
	}()
	client := &OmoiClient{}
	reply, err := client.consumeSSE(&streamActivityReader{Reader: reader, idle: idle, timeout: 100 * time.Millisecond})
	if err != nil || reply != "finished" || ctx.Err() != nil {
		t.Fatalf("queued stream: reply=%q err=%v context=%v", reply, err, ctx.Err())
	}
}
func TestStreamInactivityCancelsRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idle := time.AfterFunc(10*time.Millisecond, cancel)
	defer idle.Stop()
	r := &streamActivityReader{Reader: strings.NewReader("ping"), idle: idle, timeout: 20 * time.Millisecond}
	_, _ = io.ReadAll(r)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("inactive stream was not cancelled")
	}
}
