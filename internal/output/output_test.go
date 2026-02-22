package output

import "testing"

func TestSanitize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"clean string", "example.com", "example.com"},
		{"strips ESC CSI clear screen", "\x1b[2Jexample.com", "example.com"},
		{"strips ESC CSI color", "\x1b[31mred\x1b[0m", "red"},
		{"strips null bytes", "example\x00.com", "example.com"},
		{"strips bell", "\x07example.com", "example.com"},
		{"strips DEL", "example\x7f.com", "example.com"},
		{"strips bare ESC", "\x1bexample.com", "example.com"},
		{"strips OSC title BEL", "\x1b]0;evil title\x07example.com", "example.com"},
		{"strips OSC title ST", "\x1b]0;evil title\x1b\\example.com", "example.com"},
		{"strips mixed control chars", "\x01\x02\x03hello\x04", "hello"},
		{"strips DCS sequence", "\x1bPpayload\x1b\\example.com", "example.com"},
		{"strips APC sequence", "\x1b_appdata\x1b\\example.com", "example.com"},
		{"strips PM sequence", "\x1b^private\x1b\\example.com", "example.com"},
		{"strips SOS sequence", "\x1bXstring\x1b\\example.com", "example.com"},
		{"strips ESC from SS2", "\x1bNGexample.com", "NGexample.com"},
		{"strips ESC from SS3", "\x1bOPexample.com", "OPexample.com"},
		{"empty string", "", ""},
		{"unicode preserved", "münchen.de", "münchen.de"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Sanitize(tt.input)
			if got != tt.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
