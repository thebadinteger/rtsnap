package rtsp

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

type Auth struct {
	Username string
	Password string
	Method   string
	Realm    string
	Nonce    string
	Qop      string
	Opaque   string
	Algo     string
	NC       uint32
	Cnonce   string
}

func parseAuthHeader(header string) map[string]string {
	params := make(map[string]string)
	header = strings.TrimSpace(header)
	space := strings.IndexByte(header, ' ')
	if space == -1 {
		return params
	}
	params["_scheme"] = strings.ToLower(header[:space])
	raw := header[space+1:]

	for len(raw) > 0 {
		raw = strings.TrimLeft(raw, ", ")
		eq := strings.IndexByte(raw, '=')
		if eq == -1 {
			break
		}
		key := strings.ToLower(strings.TrimSpace(raw[:eq]))
		raw = strings.TrimLeft(raw[eq+1:], " ")

		var val string
		if len(raw) > 0 && raw[0] == '"' {
			raw = raw[1:]
			end := strings.IndexByte(raw, '"')
			if end == -1 {
				val = raw
				raw = ""
			} else {
				val = raw[:end]
				raw = raw[end+1:]
			}
		} else {
			end := strings.IndexAny(raw, ", ")
			if end == -1 {
				val = raw
				raw = ""
			} else {
				val = raw[:end]
				raw = raw[end:]
			}
		}
		params[key] = val
	}
	return params
}

// build auth from server challenge
func NewAuth(wwwAuth, username, password string) *Auth {
	params := parseAuthHeader(wwwAuth)
	scheme := params["_scheme"]
	if scheme != "digest" && scheme != "basic" {
		return nil
	}

	algo := strings.ToUpper(strings.TrimSpace(params["algorithm"]))
	switch algo {
	case "":
		algo = "MD5"
	case "MD5", "MD5-SESS", "SHA-256", "SHA-256-SESS":
	default:
		return nil
	}

	// only qop auth is implemented, refuse anything else explicitly
	qop := strings.TrimSpace(params["qop"])
	if qop != "" {
		supported := false
		for _, v := range strings.Split(qop, ",") {
			if strings.EqualFold(strings.TrimSpace(v), "auth") {
				supported = true
				break
			}
		}
		if !supported {
			return nil
		}
	}

	return &Auth{
		Username: username,
		Password: password,
		Method:   scheme,
		Realm:    params["realm"],
		Nonce:    params["nonce"],
		Qop:      qop,
		Opaque:   params["opaque"],
		Algo:     algo,
	}
}

// prefer digest when the server offers several challenges
func pickAuthChallenge(values []string) string {
	basic := ""
	for _, v := range values {
		params := parseAuthHeader(v)
		switch params["_scheme"] {
		case "digest":
			return v
		case "basic":
			if basic == "" {
				basic = v
			}
		}
	}
	return basic
}

// server asks to repeat auth with a fresh nonce
func staleChallenge(header string) bool {
	params := parseAuthHeader(header)
	return strings.EqualFold(strings.TrimSpace(params["stale"]), "true")
}

func (a *Auth) hash(data string) string {
	if strings.HasPrefix(a.Algo, "SHA-256") {
		h := sha256.Sum256([]byte(data))
		return hex.EncodeToString(h[:])
	}
	h := md5.Sum([]byte(data))
	return hex.EncodeToString(h[:])
}

func randomCnonce() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// render auth header value
func (a *Auth) Generate(method, uri string) string {
	if a.Method == "basic" {
		creds := a.Username + ":" + a.Password
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
	}

	var parts []string
	parts = append(parts, fmt.Sprintf(`username="%s"`, a.Username))
	parts = append(parts, fmt.Sprintf(`realm="%s"`, a.Realm))
	parts = append(parts, fmt.Sprintf(`nonce="%s"`, a.Nonce))
	parts = append(parts, fmt.Sprintf(`uri="%s"`, uri))

	hasQop := false
	for _, q := range strings.Split(a.Qop, ",") {
		if strings.TrimSpace(q) == "auth" {
			hasQop = true
			break
		}
	}

	if a.Cnonce == "" {
		a.Cnonce = randomCnonce()
	}

	ha1 := a.hash(a.Username + ":" + a.Realm + ":" + a.Password)
	if strings.HasSuffix(strings.ToLower(a.Algo), "-sess") {
		ha1 = a.hash(ha1 + ":" + a.Nonce + ":" + a.Cnonce)
	}
	ha2 := a.hash(method + ":" + uri)

	var response string
	if hasQop {
		a.NC++
		nc := fmt.Sprintf("%08x", a.NC)
		response = a.hash(ha1 + ":" + a.Nonce + ":" + nc + ":" + a.Cnonce + ":auth:" + ha2)
		parts = append(parts, `qop=auth`)
		parts = append(parts, fmt.Sprintf(`nc=%s`, nc))
		parts = append(parts, fmt.Sprintf(`cnonce="%s"`, a.Cnonce))
	} else {
		response = a.hash(ha1 + ":" + a.Nonce + ":" + ha2)
	}

	parts = append(parts, fmt.Sprintf(`response="%s"`, response))
	if a.Algo != "" && a.Algo != "MD5" {
		parts = append(parts, fmt.Sprintf(`algorithm="%s"`, a.Algo))
	}
	if a.Opaque != "" {
		parts = append(parts, fmt.Sprintf(`opaque="%s"`, a.Opaque))
	}

	return "Digest " + strings.Join(parts, ", ")
}
