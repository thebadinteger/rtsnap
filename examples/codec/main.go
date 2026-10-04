package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/thebadinteger/rtsnap"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// explicitly request mjpeg codec or fallback/fail if unavailable
	data, err := rtsnap.SnapshotJPEG(
		ctx,
		"rtsp://localhost:8554/live",
		85,
		rtsnap.WithCodec(rtsnap.CodecMJPEG),
		rtsnap.WithTimeout(5*time.Second),
	)
	if err != nil {
		fmt.Printf("mjpeg capture failed: %v\n", err)

		// capture with auto-selected codec (h264 > h265 > mjpeg)
		fmt.Println("retrying with auto codec selection...")
		data, err = rtsnap.SnapshotJPEG(ctx, "rtsp://localhost:8554/live", 85)
		if err != nil {
			panic(err)
		}
	}

	_ = os.WriteFile("snapshot.jpg", data, 0644)
	fmt.Printf("captured %d bytes\n", len(data))
}
