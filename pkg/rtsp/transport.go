package rtsp

// rtp transport selector
type Transport string

const (
	TransportTCP  Transport = "tcp"
	TransportUDP  Transport = "udp"
	TransportAuto Transport = "auto"
)
