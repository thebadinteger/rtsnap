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

	// snapshot with basic or digest authentication
	jpegBytes, err := rtsnap.SnapshotJPEG(
		ctx,
		"rtsp://192.168.1.100:554/cam1",
		90,
		rtsnap.WithAuth("admin", "secret123"),
		rtsnap.WithTimeout(5*time.Second),
	)
	if err != nil {
		panic(err)
	}

	if err := os.WriteFile("auth_snapshot.jpg", jpegBytes, 0644); err != nil {
		panic(err)
	}
	fmt.Println("saved to auth_snapshot.jpg")
}
