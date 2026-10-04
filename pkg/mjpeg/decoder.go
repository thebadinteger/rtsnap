package mjpeg

import (
	"bytes"
	"image"
	"image/jpeg"
)

type Decoder struct{}

// fresh mjpeg decoder
func NewDecoder() *Decoder {
	return &Decoder{}
}

// decode jpeg bytes into image
func (d *Decoder) Decode(data []byte) (image.Image, error) {
	return jpeg.Decode(bytes.NewReader(data))
}
