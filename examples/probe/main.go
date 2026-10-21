package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/thebadinteger/rtsnap/pkg/rtsp"
)

func probe(ctx context.Context, url, user, pass string) (int, string) {
	client, err := rtsp.DialWithConfig(ctx, url, user, pass, &rtsp.DialConfig{
		TLSConfig: &tls.Config{InsecureSkipVerify: true},
	})
	if err != nil {
		fmt.Printf("%s: unreachable: %v\n", url, err)
		return 0, err.Error()
	}
	defer client.Close()

	// raw describe checks credentials without triggering auth retry
	res, err := client.DescribeRaw(ctx)
	if err != nil {
		fmt.Printf("%s: probe error: %v\n", url, err)
		return 0, err.Error()
	}

	challenge := res.Header("WWW-Authenticate")
	switch res.StatusCode {
	case 200:
		fmt.Printf("%s: open, no auth required\n", url)
	case 401:
		fmt.Printf("%s: auth required, challenge: %s\n", url, challenge)
	case 403:
		fmt.Printf("%s: forbidden by address or policy\n", url)
	case 404:
		fmt.Printf("%s: stream not found\n", url)
	default:
		fmt.Printf("%s: status %d\n", url, res.StatusCode)
	}
	return res.StatusCode, challenge
}

// same probe but through the high-level error type
func probeWithError(ctx context.Context, url, user, pass string) {
	client, err := rtsp.Dial(ctx, url, user, pass, true)
	if err != nil {
		fmt.Printf("%s: unreachable: %v\n", url, err)
		return
	}
	defer client.Close()

	if err := client.Describe(ctx); err != nil {
		var statusErr *rtsp.StatusError
		if errors.As(err, &statusErr) {
			fmt.Printf("%s: rejected with %d, challenges: %v\n", url, statusErr.Code, statusErr.Values("WWW-Authenticate"))
			return
		}
		if errors.Is(err, rtsp.ErrNoMedia) {
			fmt.Printf("%s: no media tracks in sdp\n", url)
			return
		}
		fmt.Printf("%s: %v\n", url, err)
		return
	}
	fmt.Printf("%s: credentials accepted\n", url)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	probe(ctx, "rtsp://192.168.1.100:554/live", "admin", "wrong")
	probe(ctx, "rtsp://192.168.1.100:554/live", "admin", "correct")
	probeWithError(ctx, "rtsp://192.168.1.100:554/live", "admin", "wrong")
}
