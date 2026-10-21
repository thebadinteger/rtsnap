package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"time"

	"github.com/thebadinteger/rtsnap"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// camera exposed through an http tunnel on port 80
	data, err := rtsnap.SnapshotJPEG(ctx, "http://192.168.1.100:80/live", 80,
		rtsnap.WithAuth("admin", "secret123"),
		rtsnap.WithTunnel(),
	)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile("tunnel.jpg", data, 0644); err != nil {
		panic(err)
	}
	fmt.Printf("saved %d bytes to tunnel.jpg\n", len(data))

	// https tunnel with a custom tls configuration
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()

	data, err = rtsnap.SnapshotJPEG(ctx2, "https://192.168.1.101:443/live", 80,
		rtsnap.WithTunnel(),
		rtsnap.WithTLSConfig(&tls.Config{InsecureSkipVerify: true}),
	)
	if err != nil {
		panic(err)
	}
	fmt.Printf("secured tunnel returned %d bytes\n", len(data))
}
