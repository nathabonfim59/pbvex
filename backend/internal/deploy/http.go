package deploy

import "fmt"

const (
	MaxHTTPHeaderCount      = 100
	MaxHTTPHeaderNameBytes  = 256
	MaxHTTPHeaderValueBytes = 8 << 10
	MaxHTTPHeadersBytes     = 64 << 10
)

// HTTPRequestEnvelope is the representation of an HTTP request passed to an
// httpAction handler. The body is the raw bytes to avoid double JSON encoding.
type HTTPRequestEnvelope struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    []byte              `json:"body,omitempty"`
}

// HTTPResponseEnvelope is the representation of an HTTP response returned from
// an httpAction handler. The body is the raw bytes.
type HTTPResponseEnvelope struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    []byte              `json:"body,omitempty"`
}

// ValidateHTTPHeaders applies the same deterministic bounds used by the JS
// Headers implementation. Count and aggregate size are measured per value.
func ValidateHTTPHeaders(headers map[string][]string) error {
	count := 0
	total := 0
	for name, values := range headers {
		if err := ValidateHTTPHeaderName(name); err != nil {
			return err
		}
		for _, value := range values {
			if err := ValidateHTTPHeaderValue(value); err != nil {
				return err
			}
			count++
			total += len(name) + len(value)
			if count > MaxHTTPHeaderCount {
				return fmt.Errorf("HTTP headers exceed count limit")
			}
			if total > MaxHTTPHeadersBytes {
				return fmt.Errorf("HTTP headers exceed aggregate size limit")
			}
		}
	}
	return nil
}

func ValidateHTTPHeaderName(name string) error {
	if len(name) == 0 || len(name) > MaxHTTPHeaderNameBytes {
		return fmt.Errorf("invalid HTTP header name")
	}
	for i := 0; i < len(name); i++ {
		if !isHTTPTokenByte(name[i]) {
			return fmt.Errorf("invalid HTTP header name")
		}
	}
	return nil
}

func ValidateHTTPHeaderValue(value string) error {
	if len(value) > MaxHTTPHeaderValueBytes {
		return fmt.Errorf("HTTP header value exceeds size limit")
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == 0x7f || (c < 0x20 && c != '\t') {
			return fmt.Errorf("invalid HTTP header value")
		}
	}
	return nil
}

func isHTTPTokenByte(c byte) bool {
	if c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
		return true
	}
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}
