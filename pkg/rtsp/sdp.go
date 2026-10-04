package rtsp

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type MediaTrack struct {
	Type        string
	Codec       string
	PayloadType uint8
	ClockRate   int
	Control     string
	Fmtp        map[string]string
	VPS         [][]byte
	SPS         [][]byte
	PPS         [][]byte
}

func parseFmtp(params string) map[string]string {
	res := make(map[string]string)
	for _, part := range strings.Split(params, ";") {
		part = strings.TrimSpace(part)
		eq := strings.IndexByte(part, '=')
		if eq > 0 {
			k := strings.ToLower(strings.TrimSpace(part[:eq]))
			v := strings.TrimSpace(part[eq+1:])
			res[k] = v
		}
	}
	return res
}

func decodeParam(p string) ([]byte, bool) {
	clean := strings.ReplaceAll(strings.ReplaceAll(p, " ", ""), "\t", "")
	if data, err := base64.StdEncoding.DecodeString(clean); err == nil && len(data) > 0 {
		return data, true
	}
	if data, err := base64.RawStdEncoding.DecodeString(clean); err == nil && len(data) > 0 {
		return data, true
	}
	return nil, false
}

// normalize endings and unfold lines
func splitSDPLines(sdp []byte) []string {
	text := strings.ReplaceAll(string(sdp), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += strings.TrimLeft(line, " \t")
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func resolveURL(base, ref string) string {
	if ref == "*" || ref == "" {
		return base
	}
	bu, err := url.Parse(base)
	if err != nil {
		return ref
	}
	ru, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return bu.ResolveReference(ru).String()
}

type sdpSection struct {
	payloads []uint8
	control  string
	rtpmap   map[uint8]string
	fmtp     map[uint8]map[string]string
}

// extract video tracks from sdp
func ParseSDP(sdp []byte, baseURL string) ([]*MediaTrack, error) {
	lines := splitSDPLines(sdp)
	var sections []sdpSection
	var curSection *sdpSection
	globalControl := ""

	for _, line := range lines {
		if len(line) < 2 || line[1] != '=' {
			continue
		}
		prefix := line[0]
		val := line[2:]

		switch prefix {
		case 'm':
			parts := strings.Fields(val)
			if len(parts) > 0 && strings.EqualFold(parts[0], "video") {
				sec := sdpSection{
					rtpmap: make(map[uint8]string),
					fmtp:   make(map[uint8]map[string]string),
				}
				for _, ptStr := range parts[3:] {
					if pt, err := strconv.Atoi(ptStr); err == nil {
						sec.payloads = append(sec.payloads, uint8(pt))
					}
				}
				sections = append(sections, sec)
				curSection = &sections[len(sections)-1]
			} else {
				curSection = nil
			}
		case 'a':
			colon := strings.IndexByte(val, ':')
			if colon == -1 {
				continue
			}
			attrName := strings.ToLower(val[:colon])
			attrVal := val[colon+1:]

			if curSection == nil {
				if attrName == "control" {
					globalControl = attrVal
				}
				continue
			}

			switch attrName {
			case "control":
				curSection.control = attrVal
			case "rtpmap":
				fields := strings.Fields(attrVal)
				if len(fields) >= 2 {
					pt, _ := strconv.Atoi(fields[0])
					curSection.rtpmap[uint8(pt)] = fields[1]
				}
			case "fmtp":
				fields := strings.SplitN(attrVal, " ", 2)
				if len(fields) == 2 {
					pt, _ := strconv.Atoi(fields[0])
					curSection.fmtp[uint8(pt)] = parseFmtp(fields[1])
				}
			}
		}
	}

	var tracks []*MediaTrack

	for _, sec := range sections {
		ctrl := sec.control
		if ctrl == "" {
			ctrl = globalControl
		}
		resolvedControl := resolveURL(baseURL, ctrl)

		for _, pt := range sec.payloads {
			var codec string
			rate := 90000

			if rtpmapVal, ok := sec.rtpmap[pt]; ok {
				encoding := rtpmapVal
				slash := strings.IndexByte(encoding, '/')
				name := strings.ToUpper(encoding)
				if slash != -1 {
					name = strings.ToUpper(encoding[:slash])
					if parsedRate, err := strconv.Atoi(encoding[slash+1:]); err == nil {
						rate = parsedRate
					}
				}

				switch name {
				case "H264":
					codec = "h264"
				case "H265", "HEVC":
					codec = "h265"
				case "JPEG":
					codec = "mjpeg"
				}
			} else if pt == 26 {
				codec = "mjpeg"
			} else if fm := sec.fmtp[pt]; fm != nil {
				if _, ok := fm["sprop-parameter-sets"]; ok {
					codec = "h264"
				} else if _, ok := fm["sprop-sps"]; ok {
					codec = "h265"
				} else if _, ok := fm["sprop-vps"]; ok {
					codec = "h265"
				}
			}

			if codec == "" {
				continue
			}

			track := &MediaTrack{
				Type:        "video",
				Codec:       codec,
				PayloadType: pt,
				ClockRate:   rate,
				Control:     resolvedControl,
				Fmtp:        sec.fmtp[pt],
			}

			// extract parameters
			if codec == "h264" && track.Fmtp != nil {
				if spsStr, ok := track.Fmtp["sprop-parameter-sets"]; ok {
					for _, p := range strings.Split(spsStr, ",") {
						if data, ok := decodeParam(strings.TrimSpace(p)); ok {
							naluType := data[0] & 0x1F
							if naluType == 7 {
								track.SPS = append(track.SPS, data)
							} else if naluType == 8 {
								track.PPS = append(track.PPS, data)
							}
						}
					}
				}
			}

			if codec == "h265" && track.Fmtp != nil {
				for _, key := range []string{"sprop-vps", "sprop-sps", "sprop-pps"} {
					if val, ok := track.Fmtp[key]; ok {
						for _, p := range strings.Split(val, ",") {
							if data, ok := decodeParam(strings.TrimSpace(p)); ok {
								switch key {
								case "sprop-vps":
									track.VPS = append(track.VPS, data)
								case "sprop-sps":
									track.SPS = append(track.SPS, data)
								case "sprop-pps":
									track.PPS = append(track.PPS, data)
								}
							}
						}
					}
				}
			}

			tracks = append(tracks, track)
		}
	}

	if len(tracks) == 0 {
		return nil, fmt.Errorf("no video tracks found in sdp")
	}

	return tracks, nil
}

// pick best matching track
func SelectTrack(tracks []*MediaTrack, codec string) (*MediaTrack, error) {
	if len(tracks) == 0 {
		return nil, fmt.Errorf("no video tracks available")
	}

	codec = strings.ToLower(strings.TrimSpace(codec))
	if codec == "" || codec == "auto" {
		priorities := []string{"h264", "h265", "mjpeg"}
		for _, prio := range priorities {
			for _, t := range tracks {
				if t.Codec == prio {
					return t, nil
				}
			}
		}
		return tracks[0], nil
	}

	for _, t := range tracks {
		if t.Codec == codec {
			return t, nil
		}
	}

	var available []string
	for _, t := range tracks {
		available = append(available, t.Codec)
	}
	return nil, fmt.Errorf("requested codec %q not found in stream (available: %v)", codec, available)
}
