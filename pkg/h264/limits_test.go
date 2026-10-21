package h264

import (
	"testing"
	"time"
)

func TestDecodeExpGolombBypassBounded(t *testing.T) {
	data := make([]byte, 64)
	for i := range data {
		data[i] = 0xff
	}

	d, err := NewCabacDecoder(data)
	if err != nil {
		t.Fatalf("cabac: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := decodeExpGolombBypass(d, 5)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error for an oversized prefix")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("decodeExpGolombBypass did not terminate")
	}
}

func TestDecodeQPDeltaBounded(t *testing.T) {
	data := make([]byte, 64)
	for i := range data {
		data[i] = 0xff
	}

	cab, err := NewCabacDecoder(data)
	if err != nil {
		t.Fatalf("cabac: %v", err)
	}

	sc := &SliceContext{Cabac: cab, Ctx: &[1024]CtxState{}, BitDepthY: 8}

	done := make(chan struct{})
	go func() {
		_, _ = DecodeQPDelta(sc)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("DecodeQPDelta did not terminate")
	}
}
