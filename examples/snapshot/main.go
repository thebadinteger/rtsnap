package main

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"time"

	"github.com/thebadinteger/rtsnap"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// capture single image frame
	img, err := rtsnap.Snapshot(ctx, "rtsp://localhost:8554/live")
	if err != nil {
		panic(err)
	}

	bounds := img.Bounds()
	fmt.Printf("captured image: %dx%d\n", bounds.Dx(), bounds.Dy())

	f, err := os.Create("snapshot.png")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
	fmt.Println("saved to snapshot.png")
}
