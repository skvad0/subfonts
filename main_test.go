package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// --- basicNormalize ---

func TestBasicNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Arial", "arial"},
		{"Open Sans", "opensans"},
		{"Noto-Serif", "notoserif"},
		{"Roboto_Mono", "robotomono"},
		{"MyriadPro Bold", "myriadprobold"},
		{"", ""},
	}
	for _, c := range cases {
		if got := basicNormalize(c.in); got != c.want {
			t.Errorf("basicNormalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- parseSRT ---

func writeTempFile(t *testing.T, content string) *os.File {
	t.Helper()
	f, err := os.CreateTemp("", "subfonts_test_*.srt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatalf("seek temp file: %v", err)
	}
	return f
}

func TestParseSRT(t *testing.T) {
	content := `1
00:00:01,000 --> 00:00:04,000
<font face="Arial">Hello world</font>

2
00:00:05,000 --> 00:00:08,000
<font face='Open Sans'>Goodbye</font>

3
00:00:09,000 --> 00:00:12,000
No font tag here
`
	f := writeTempFile(t, content)
	fonts := make(map[string]bool)
	if err := parseSRT(f, fonts); err != nil {
		t.Fatalf("parseSRT error: %v", err)
	}

	want := []string{"Arial", "Open Sans"}
	for _, w := range want {
		if !fonts[w] {
			t.Errorf("expected font %q not found; got %v", w, fonts)
		}
	}
	if len(fonts) != len(want) {
		t.Errorf("got %d fonts, want %d: %v", len(fonts), len(want), fonts)
	}
}

// --- parseASS ---

func TestParseASS(t *testing.T) {
	content := `[Script Info]
Title: Test

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour
Style: Default,Noto Sans,48,&H00FFFFFF
Style: Signs,Open Sans,36,&H00FFFFFF

[Events]
Format: Layer, Start, End, Style, Text
Dialogue: 0,0:00:01.00,0:00:04.00,Default,,Hello
`
	f := writeTempFile(t, content)
	// rename the temp file to .ass so extension detection works if needed
	// (parseASS doesn't use the extension, so this is fine)
	fonts := make(map[string]bool)
	if err := parseASS(f, fonts); err != nil {
		t.Fatalf("parseASS error: %v", err)
	}

	want := []string{"Noto Sans", "Open Sans"}
	for _, w := range want {
		if !fonts[w] {
			t.Errorf("expected font %q not found; got %v", w, fonts)
		}
	}
	if len(fonts) != len(want) {
		t.Errorf("got %d fonts, want %d: %v", len(fonts), len(want), fonts)
	}
}

// --- normalizeFontsWithAI (unit test with mock server) ---

// mockOllama starts a local HTTP server that returns a predetermined AI response.
// The returned URL can be swapped into the function via the package-level var ollamaURL.
func mockOllama(t *testing.T, correctedMap map[string]string) *httptest.Server {
	t.Helper()
	inner, _ := json.Marshal(correctedMap)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := OllamaResponse{Response: string(inner)}
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNormalizeFontsWithAI_Mock(t *testing.T) {
	want := map[string]string{
		"Arial-Bold-MT":    "Arial",
		"OpenSans-Regular": "Open Sans",
	}
	srv := mockOllama(t, want)

	// Temporarily point the function at our mock server.
	old := ollamaURL
	ollamaURL = srv.URL + "/api/generate"
	defer func() { ollamaURL = old }()

	input := []string{"Arial-Bold-MT", "OpenSans-Regular"}
	got, err := normalizeFontsWithAI(input)
	if err != nil {
		t.Fatalf("normalizeFontsWithAI error: %v", err)
	}

	for k, wantV := range want {
		if got[k] != wantV {
			t.Errorf("key %q: got %q, want %q", k, got[k], wantV)
		}
	}
}

// --- normalizeFontsWithAI (integration test — requires Ollama running locally) ---

func TestNormalizeFontsWithAI_Integration(t *testing.T) {
	if strings.ToLower(os.Getenv("OLLAMA_INTEGRATION")) != "true" {
		t.Skip("set OLLAMA_INTEGRATION=true to run this test against a live Ollama instance")
	}

	input := []string{"Arial-Bold-MT", "OpenSans-Regular", "NotoSans-Italic"}
	t.Logf("Sending to Ollama: %v", input)

	got, err := normalizeFontsWithAI(input)
	if err != nil {
		t.Fatalf("normalizeFontsWithAI error: %v", err)
	}

	t.Logf("=== Ollama raw response map ===")
	for k, v := range got {
		t.Logf("  %q -> %q", k, v)
	}

	if len(got) == 0 {
		t.Error("expected non-empty response map")
	}
}
