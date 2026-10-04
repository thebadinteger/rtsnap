package main

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"time"

	"github.com/thebadinteger/rtsnap/pkg/h264"
	"github.com/thebadinteger/rtsnap/pkg/rtp"
	"github.com/thebadinteger/rtsnap/pkg/rtsp"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := rtsp.Dial(ctx, "rtsp://localhost:8554/h264", "", "")
	if err != nil {
		panic(err)
	}
	defer client.Close()

	if err := client.Describe(ctx); err != nil {
		panic(err)
	}

	if client.Track == nil || client.Track.Codec != "h264" {
		panic("expected h264 video track")
	}

	if err := client.Setup(ctx); err != nil {
		panic(err)
	}

	if err := client.Play(ctx); err != nil {
		panic(err)
	}
	defer client.Teardown(ctx)

	depack := rtp.NewH264Depacketizer()
	dec := h264.New()

	// feed initial sps and pps from sdp if present
	if len(client.Track.SPS) > 0 {
		_, _ = dec.DecodeNALUs(client.Track.SPS)
	}
	if len(client.Track.PPS) > 0 {
		_, _ = dec.DecodeNALUs(client.Track.PPS)
	}

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

		nalus, err := depack.Decode(&pkt)
		if err != nil || len(nalus) == 0 {
			continue
		}

		f, err := dec.DecodeNALUs(nalus)
		if err == nil && f != nil {
			img := f.Image()
			if img != nil {
				out, err := os.Create("h264_frame.png")
				if err != nil {
					panic(err)
				}
				defer out.Close()

				if err := png.Encode(out, img); err != nil {
					panic(err)
				}
				fmt.Println("saved h264 frame to h264_frame.png")
				return
			}
		}
	}
}
