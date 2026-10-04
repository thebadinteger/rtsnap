package main

import (
	"context"
	"fmt"
	"time"

	"github.com/thebadinteger/rtsnap"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// query stream tracks without capturing frames
	info, err := rtsnap.Query(ctx, "rtsp://localhost:8554/live")
	if err != nil {
		panic(err)
	}

	fmt.Printf("stream url: %s\n", info.URL)
	fmt.Printf("found %d video track(s):\n", len(info.Tracks))
	for i, trk := range info.Tracks {
		fmt.Printf("  [%d] codec=%s payload=%d clock=%d control=%s\n",
			i, trk.Codec, trk.PayloadType, trk.ClockRate, trk.Control)
	}
}
