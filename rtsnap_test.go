package rtsnap

import (
	"errors"
	"testing"

	"github.com/thebadinteger/rtsnap/pkg/h264"
	"github.com/thebadinteger/rtsnap/pkg/h265"
)

func TestSafelyPassThrough(t *testing.T) {
	v, err := safely(func() (int, error) { return 7, nil })
	if err != nil || v != 7 {
		t.Fatalf("expected (7, nil), got (%d, %v)", v, err)
	}

	want := errors.New("boom")
	_, err = safely(func() (int, error) { return 0, want })
	if err != want {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestSafelyRecoversPanic(t *testing.T) {
	_, err := safely(func() (*h264.Frame, error) { panic("bad frame") })
	if err == nil {
		t.Fatal("expected error for panicking decode, got nil")
	}

	_, err = safely(func() ([]*h265.Picture, error) { panic("bad picture") })
	if err == nil {
		t.Fatal("expected error for panicking decode, got nil")
	}
}
