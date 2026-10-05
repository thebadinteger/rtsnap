package main

import (
	"context"
	"fmt"
	"time"

	"github.com/thebadinteger/rtsnap"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// print raw rtsp exchange while capturing
	_, err := rtsnap.Snapshot(ctx, "rtsp://localhost:8554/stream",
		rtsnap.WithDebugFunc(func(s string) {
			fmt.Println(s)
		}),
	)
	if err != nil {
		panic(err)
	}
}
