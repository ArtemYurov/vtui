package vtui

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected and returns what was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stdout = orig
	w.Close()
	out := <-done
	r.Close()
	return out
}

// withTerminalClipboard restores the global flag so these tests do not leak
// their state into the rest of the suite.
func withTerminalClipboard(t *testing.T, disabled bool) {
	t.Helper()
	prev := noTerminalBehind.Load()
	noTerminalBehind.Store(disabled)
	t.Cleanup(func() { noTerminalBehind.Store(prev) })
}

// In a GUI window the OSC 52 escape has no terminal to reach: it would land in
// the shell the application was launched from and print as garbage.
func TestSetClipboard_NoOSC52WhenTerminalDisabled(t *testing.T) {
	withTerminalClipboard(t, true)
	testSkipOSClipboard = true
	defer func() { testSkipOSClipboard = false }()

	out := captureStdout(t, func() { SetClipboard("secret text") })

	if strings.Contains(out, "\x1b]52") {
		t.Errorf("OSC 52 was emitted with no terminal attached: %q", out)
	}
	if out != "" {
		t.Errorf("expected nothing on stdout, got %q", out)
	}
}

// With a terminal attached the escape must still be the last resort, so the
// GUI change does not quietly disable clipboard support for terminal users.
func TestSetClipboard_EmitsOSC52WhenTerminalPresent(t *testing.T) {
	testSkipOSClipboard = true
	defer func() { testSkipOSClipboard = false }()
	withTerminalClipboard(t, false)

	out := captureStdout(t, func() { SetClipboard("hello") })

	if !strings.Contains(out, "\x1b]52;c;") {
		t.Errorf("expected an OSC 52 sequence on stdout, got %q", out)
	}
}

// Copy and paste must keep working inside the application even when no OS
// clipboard helper exists and the escape fallback is suppressed.
func TestClipboard_RoundTripsThroughInternalBuffer(t *testing.T) {
	withTerminalClipboard(t, true)
	testSkipOSClipboard = true
	defer func() { testSkipOSClipboard = false }()

	const want = "выделенный текст\nвторая строка"
	captureStdout(t, func() { SetClipboard(want) })

	if got := GetClipboard(); got != want {
		t.Errorf("GetClipboard() = %q, want %q", got, want)
	}
}

func TestDisableTerminalClipboard(t *testing.T) {
	withTerminalClipboard(t, false)

	if TerminalClipboardDisabled() {
		t.Fatal("expected the terminal fallback to start enabled")
	}
	DisableTerminalClipboard()
	if !TerminalClipboardDisabled() {
		t.Error("DisableTerminalClipboard did not take effect")
	}
}

// The 2MB cap has to survive the early return, or a GUI session could stash an
// unbounded string in the internal buffer.
func TestSetClipboard_TruncatesOversizedTextInGUIMode(t *testing.T) {
	withTerminalClipboard(t, true)
	testSkipOSClipboard = true
	defer func() { testSkipOSClipboard = false }()

	const limit = 2 * 1024 * 1024
	captureStdout(t, func() { SetClipboard(strings.Repeat("x", limit+4096)) })

	if got := len(GetClipboard()); got != limit {
		t.Errorf("stored %d bytes, want the %d byte cap", got, limit)
	}
}

// Every GUI host has to turn the OSC 52 fallback off, and three of them did
// not: gogpu, X11 and Wayland left it on, so a copy in a window with no
// clipboard helper installed wrote an escape sequence into the shell the
// application was started from.
//
// The check reads the sources because the hosts themselves cannot run here:
// each one needs a display, a GPU stack or a compositor. A new backend is not
// covered until it is added to this list, which is the point at which someone
// has to think about the question.
func TestGUIHostsDisableTheTerminalClipboard(t *testing.T) {
	hosts := []string{
		"ebiten_host.go",
		"gogpu_host.go",
		"wayland_host.go",
		"win32_gui_windows.go",
		"x11_host.go",
	}

	for _, name := range hosts {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(string(src), "DisableTerminalClipboard()") {
			t.Errorf("%s never calls DisableTerminalClipboard: a window would emit OSC 52 at the shell behind it", name)
		}
	}
}
