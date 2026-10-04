package main

import (
	"context"
	"fmt"
	"time"

	"github.com/thebadinteger/rtsnap/pkg/rtp"
	"github.com/thebadinteger/rtsnap/pkg/rtsp"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// connect to rtsp server
	client, err := rtsp.Dial(ctx, "rtsp://localhost:8554/stream", "", "")
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// describe stream and inspect track info
	if err := client.Describe(ctx); err != nil {
		panic(err)
	}
	fmt.Printf("stream codec: %s, control: %s\n", client.Track.Codec, client.Track.Control)

	// setup tcp interleaved transport
	if err := client.Setup(ctx); err != nil {
		panic(err)
	}

	// start playback
	if err := client.Play(ctx); err != nil {
		panic(err)
	}
	defer client.Teardown(ctx)

	// read first incoming rtp packet
	frame, err := client.ReadFrame(ctx)
	if err != nil {
		panic(err)
	}

	var pkt rtp.Packet
	if err := pkt.Unmarshal(frame.Payload); err != nil {
		panic(err)
	}

	fmt.Printf("received rtp packet: seq=%d timestamp=%d payload=%d bytes\n",
		pkt.SequenceNumber, pkt.Timestamp, len(pkt.Payload))
}
