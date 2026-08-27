package k8s

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// The Keycloak image has no tar.
//
// `kubectl cp` and everything built the way it is built run tar inside the
// container, and the official image is assembled on ubi-micro, which ships
// neither tar nor gzip. Every Kubernetes capture therefore ran the export
// successfully and then failed collecting its output, reporting only that the
// stream had ended — because tar's stderr was going to io.Discard.
//
// These drive the framing that replaced it. The fixture in testdata was
// produced by running the real script, verbatim, inside a real Keycloak pod.

func readFrames(t *testing.T, r io.Reader) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if err == io.EOF && strings.TrimSpace(line) == "" {
			return out
		}
		if err != nil {
			t.Fatalf("reading a header: %v", err)
		}
		name, size, perr := parseFrame(line)
		if perr != nil {
			t.Fatalf("parsing %q: %v", line, perr)
		}
		body := make([]byte, size)
		if _, err := io.ReadFull(br, body); err != nil {
			t.Fatalf("reading %d bytes of %s: %v", size, name, err)
		}
		out[name] = body
	}
}

// The stream a real Keycloak pod produced, parsed back byte for byte.
func TestCopyOutFraming_ReadsWhatARealPodSent(t *testing.T) {
	f, err := os.Open("testdata/frames.bin")
	if err != nil {
		t.Fatalf("the fixture is missing: %v", err)
	}
	defer f.Close() //nolint:errcheck

	files := readFrames(t, f)
	if len(files) != 3 {
		t.Fatalf("expected three files, got %d: %v", len(files), keys(files))
	}
	if got := string(files["corp-a-realm.json"]); !strings.Contains(got, `"realm":"corp-a"`) {
		t.Errorf("the realm file did not survive: %q", got)
	}
	// A NUL and a 0x01 in the middle: the framing carries bytes, not lines.
	if got := files["bin.dat"]; !bytes.Equal(got, []byte{'a', 0x00, 0x01, 'b'}) {
		t.Errorf("binary content was mangled: %v", got)
	}
	// Large enough to cross the reader's buffer several times, which is where a
	// length-framed reader gets it wrong if it gets it wrong at all.
	if got := len(files["big.dat"]); got != 200000 {
		t.Errorf("the large file arrived as %d bytes, not 200000", got)
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The script is the contract. It may use only what a minimal image has, which
// is the whole reason it exists — reaching for tar again would reintroduce the
// bug in a form nothing here would catch.
func TestCopyOutScript_UsesNothingTheImageLacks(t *testing.T) {
	script := copyOutScript("/tmp/portcloak-1/corp-a")

	if !strings.Contains(script, "'/tmp/portcloak-1/corp-a'") {
		t.Errorf("the directory is not quoted, so a path with a space would split: %s", script)
	}
	for _, forbidden := range []string{"tar", "gzip", "awk", "python"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("the script uses %q, which the Keycloak image does not have: %s", forbidden, script)
		}
	}
	for _, needed := range []string{"find", "wc", "cat", "printf"} {
		if !strings.Contains(script, needed) {
			t.Errorf("the script no longer uses %q; the framing depends on it: %s", needed, script)
		}
	}
	if strings.Contains(copyInScript("/tmp/x/bundle.pcz"), "tar") {
		t.Error("the restore path went back to tar")
	}
}

func TestParseFrame_RefusesWhatItCannotTrust(t *testing.T) {
	if name, size, err := parseFrame("PCF 12 corp-a-users-0.json\n"); err != nil ||
		name != "corp-a-users-0.json" || size != 12 {
		t.Errorf("a good header did not parse: %q %d %v", name, size, err)
	}
	// A name with spaces survives, because only the first two fields are split.
	if name, _, err := parseFrame("PCF 4 a file.json\n"); err != nil || name != "a file.json" {
		t.Errorf("a name with a space was mangled: %q %v", name, err)
	}
	for _, bad := range []string{
		"sh: tar: command not found\n",
		"PCF notanumber x.json\n",
		"PCF -1 x.json\n",
		"PCF 4\n",
		"\n",
	} {
		if _, _, err := parseFrame(bad); err == nil {
			t.Errorf("%q was accepted as a file header", bad)
		}
	}
}

// The stderr that used to be discarded is the half that names the cause.
func TestBoundedBuffer_KeepsTheFirstLineAndStopsGrowing(t *testing.T) {
	var b boundedBuffer
	_, _ = b.Write([]byte("sh: line 1: tar: command not found\nand more\n"))
	if got := b.suffix(); !strings.Contains(got, "tar: command not found") {
		t.Errorf("the reason was not kept: %q", got)
	}
	if strings.Contains(b.suffix(), "and more") {
		t.Errorf("more than the first line is being shown: %q", b.suffix())
	}

	var big boundedBuffer
	for range 100 {
		if _, err := big.Write(bytes.Repeat([]byte("x"), 4096)); err != nil {
			t.Fatal(err)
		}
	}
	if big.buf.Len() > 8<<10 {
		t.Errorf("the buffer grew to %d bytes; a command that fails by talking forever must not", big.buf.Len())
	}

	var quiet boundedBuffer
	if quiet.suffix() != "" {
		t.Errorf("a command that said nothing is adding %q to the failure", quiet.suffix())
	}
}
