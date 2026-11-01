package api

import "testing"

func TestPrintable(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ in, want string }{
		{"plain /home/alice/notes.txt", "plain /home/alice/notes.txt"},
		{"ty\u00f6 \u2603 \U0001F413 \u65e5\u672c\u8a9e", "ty\u00f6 \u2603 \U0001F413 \u65e5\u672c\u8a9e"},
		{"a\nb", `a\nb`},
		{"a\r\nb", `a\r\nb`},
		{"a\tb", `a\tb`},
		{"\x1b[31mred", `\u001b[31mred`},
		{"nul\x00here", `nul\u0000here`},
		{"del\x7f", `del\u007f`},
		{"photo\u202egpj.exe", `photo\u202egpj.exe`},
		{"\u2066x\u2069", `\u2066x\u2069`},
		{"a\u200eb", `a\u200eb`},
		{"bad \xff bytes", "bad \uFFFD bytes"},
		// A zero-width joiner makes one emoji of several, so it stays.
		{"\U0001F468\u200d\U0001F469\u200d\U0001F467", "\U0001F468\u200d\U0001F469\u200d\U0001F467"},
		{"", ""},
	} {
		if got := printable(tt.in); got != tt.want {
			t.Errorf("printable(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
