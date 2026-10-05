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

	// try udp first then fall back to tcp
	data, err := rtsnap.SnapshotJPEG(ctx, "rtsp://localhost:8554/stream", 80,
		rtsnap.WithTransport(rtsnap.TransportAuto),
	)
	if err != nil {
		panic(err)
	}

	if err := os.WriteFile("output.jpg", data, 0644); err != nil {
		panic(err)
	}
	fmt.Printf("saved %d bytes to output.jpg\n", len(data))
}
