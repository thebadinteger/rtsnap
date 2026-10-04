package mjpeg

import (
	"bytes"
	"image"
	"image/jpeg"
)

type Decoder struct{}

func NewDecoder() *Decoder {
	return &Decoder{}
}

// decode parses jpeg bitstream into image.image
func (d *Decoder) Decode(data []byte) (image.Image, error) {
	return jpeg.Decode(bytes.NewReader(data))
}
