package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/thebadinteger/rtsnap"
)

func main() {
	// create context with strict deadline
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := rtsnap.Snapshot(ctx, "rtsp://10.255.255.1:554/live", rtsnap.WithTimeout(2*time.Second))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			fmt.Println("snapshot timed out as expected")
			return
		}
		fmt.Println("failed with error:", err)
	}
}
