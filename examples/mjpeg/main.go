package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/thebadinteger/rtsnap/pkg/mjpeg"
	"github.com/thebadinteger/rtsnap/pkg/rtp"
	"github.com/thebadinteger/rtsnap/pkg/rtsp"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := rtsp.Dial(ctx, "rtsp://localhost:8554/mjpeg", "", "", true)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	if err := client.Describe(ctx); err != nil {
		panic(err)
	}

	if client.Track == nil || client.Track.Codec != "mjpeg" {
		panic("expected mjpeg video track")
	}

	if err := client.Setup(ctx); err != nil {
		panic(err)
	}

	if err := client.Play(ctx); err != nil {
		panic(err)
	}
	defer client.Teardown(ctx)

	depack := rtp.NewMJPEGDepacketizer()
	dec := mjpeg.NewDecoder()

	for {
		frame, err := client.ReadFrame(ctx)
		if err != nil {
			panic(err)
		}
		if frame.Channel != client.RTPChannel() {
			continue
		}

		var pkt rtp.Packet
		if err := pkt.Unmarshal(frame.Payload); err != nil {
			continue
		}

		jpegBytes, err := depack.Decode(&pkt)
		if err != nil || len(jpegBytes) == 0 {
			continue
		}

		img, err := dec.Decode(jpegBytes)
		if err != nil {
			continue
		}

		bounds := img.Bounds()
		fmt.Printf("decoded mjpeg frame: %dx%d\n", bounds.Dx(), bounds.Dy())

		if err := os.WriteFile("mjpeg_frame.jpg", jpegBytes, 0644); err != nil {
			panic(err)
		}
		fmt.Println("saved to mjpeg_frame.jpg")
		return
	}
}
