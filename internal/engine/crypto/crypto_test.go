// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package crypto_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"portcloak/internal/engine/crypto"
	"portcloak/internal/engine/snapshot"
)

func sealFixture(t *testing.T, cfg crypto.Config) ([]byte, snapshot.Encryption) {
	t.Helper()
	b, err := snapshot.NewBuilder(filepath.Join(t.TempDir(), "stage"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Stage(context.Background(), "realm/acme-realm.json",
		strings.NewReader(`{"realm":"acme","clients":[{"clientId":"app-web","secret":"a-real-secret"}]}`)); err != nil {
		t.Fatal(err)
	}
	tree := b.Tree()
	if _, err := b.Document(snapshot.IntegrityPath, tree); err != nil {
		t.Fatal(err)
	}
	sealer, err := crypto.NewSealer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	enc := snapshot.Encryption{}
	if sealer != nil {
		enc = sealer.Describe()
	}
	if _, err := b.Document(snapshot.EnvelopePath, snapshot.Envelope{
		SchemaVersion: snapshot.SchemaVersion, SnapshotID: "01HZY3", Realm: "acme",
		CreatedAt: time.Unix(0, 0).UTC(), IntegrityRoot: tree.Root, Encryption: enc,
	}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	// A nil *Sealer would still satisfy the interface and then panic, so the
	// unencrypted path passes an explicitly nil interface value.
	if sealer == nil {
		if _, err := b.Seal(context.Background(), &buf, nil); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := b.Seal(context.Background(), &buf, sealer); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes(), enc
}

func TestCrypto_RoundTrip_Passphrase(t *testing.T) {
	cfg := crypto.Config{Enabled: true, Mode: snapshot.EncryptionPassphrase, Passphrase: "correct horse battery staple"}
	sealed, enc := sealFixture(t, cfg)

	if !enc.Enabled || enc.Mode != snapshot.EncryptionPassphrase {
		t.Fatalf("envelope recorded %+v", enc)
	}
	// The sealed bytes must not be readable without the passphrase.
	if bytes.Contains(sealed, []byte("a-real-secret")) {
		t.Fatal("the secret is present in the sealed bundle in the clear")
	}
	if _, err := snapshot.ReadEnvelopeOnly(context.Background(), bytes.NewReader(sealed), nil); err == nil {
		t.Fatal("an encrypted bundle was readable with no key")
	}

	opener, err := crypto.OpenerFor(enc, "correct horse battery staple", nil)
	if err != nil {
		t.Fatal(err)
	}
	opened := open(t, sealed, opener)
	defer func() { _ = opened.Close() }()
	if opened.Envelope.Realm != "acme" || !opened.Verify.OK {
		t.Fatalf("round trip failed: %+v / %s", opened.Envelope, opened.Verify.Message)
	}
}

func TestCrypto_RoundTrip_Recipients(t *testing.T) {
	priv1, pub1, err := crypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	priv2, pub2, err := crypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}

	cfg := crypto.Config{Enabled: true, Mode: snapshot.EncryptionRecipients, Recipients: []string{pub1, pub2}}
	sealed, enc := sealFixture(t, cfg)

	if len(enc.Recipients) != 2 {
		t.Fatalf("the envelope recorded %d recipients", len(enc.Recipients))
	}

	// Each listed recipient opens it with their own private key, which is what
	// separates who can capture from who can restore.
	for _, key := range []string{priv1, priv2} {
		opener, err := crypto.OpenerFor(enc, "", []string{key})
		if err != nil {
			t.Fatal(err)
		}
		opened := open(t, sealed, opener)
		if opened.Envelope.Realm != "acme" {
			t.Fatalf("recipient could not read the bundle: %+v", opened.Envelope)
		}
		_ = opened.Close()
	}

	// Someone who is not a recipient cannot.
	stranger, _, err := crypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	opener, err := crypto.OpenerFor(enc, "", []string{stranger})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.Open(context.Background(), bytes.NewReader(sealed),
		snapshot.OpenOptions{Dir: t.TempDir(), Opener: opener}); err == nil {
		t.Fatal("a non-recipient decrypted the bundle")
	}
}

// A wrong key is one clear failure, not five slow ones.
func TestCrypto_WrongKey(t *testing.T) {
	cfg := crypto.Config{Enabled: true, Mode: snapshot.EncryptionPassphrase, Passphrase: "right"}
	sealed, enc := sealFixture(t, cfg)

	opener, err := crypto.OpenerFor(enc, "wrong", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = snapshot.Open(context.Background(), bytes.NewReader(sealed),
		snapshot.OpenOptions{Dir: t.TempDir(), Opener: opener})
	if err == nil {
		t.Fatal("a wrong passphrase opened the bundle")
	}
	if !errors.Is(err, crypto.ErrWrongKey) {
		t.Errorf("the failure is not recognisable as a wrong key: %v", err)
	}
	if !strings.Contains(err.Error(), "could not be decrypted") {
		t.Errorf("the message is not a sentence about the operator's situation: %v", err)
	}
}

func TestCrypto_MissingKeyIsAClearMessageNotAParseError(t *testing.T) {
	enc := snapshot.Encryption{Enabled: true, Mode: snapshot.EncryptionPassphrase}
	if _, err := crypto.OpenerFor(enc, "", nil); err == nil {
		t.Fatal("opening a passphrase bundle with no passphrase was allowed")
	} else if !strings.Contains(err.Error(), "passphrase") {
		t.Errorf("unhelpful message: %v", err)
	}

	enc = snapshot.Encryption{Enabled: true, Mode: snapshot.EncryptionRecipients, Recipients: []string{"age1x", "age1y"}}
	_, err := crypto.OpenerFor(enc, "", nil)
	if err == nil {
		t.Fatal("opening a recipient bundle with no identity was allowed")
	}
	if !strings.Contains(err.Error(), "2 recipient") {
		t.Errorf("the message should say how many recipients could open it: %v", err)
	}
}

// Save must be blocked until there is something to encrypt to, or the bundle
// would be unopenable by anyone.
func TestConfig_Validate(t *testing.T) {
	cases := []struct {
		name string
		cfg  crypto.Config
		ok   bool
	}{
		{"disabled", crypto.Config{}, true},
		{"passphrase", crypto.Config{Enabled: true, Mode: snapshot.EncryptionPassphrase, Passphrase: "x"}, true},
		{"passphrase missing", crypto.Config{Enabled: true, Mode: snapshot.EncryptionPassphrase}, false},
		{"no recipients", crypto.Config{Enabled: true, Mode: snapshot.EncryptionRecipients}, false},
		{"bad recipient", crypto.Config{Enabled: true, Mode: snapshot.EncryptionRecipients, Recipients: []string{"not-a-key"}}, false},
		{"no mode", crypto.Config{Enabled: true}, false},
	}
	for _, c := range cases {
		err := c.cfg.Validate()
		if c.ok && err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: was accepted", c.name)
		}
	}
}

// A bundle that cannot be decrypted should be found at capture, not eighteen
// months later during an incident.
func TestVerifyDecryptable_CatchesABundleNobodyCanOpen(t *testing.T) {
	cfg := crypto.Config{Enabled: true, Mode: snapshot.EncryptionPassphrase, Passphrase: "right"}
	sealed, _ := sealFixture(t, cfg)

	openGood := func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(sealed)), nil }
	if err := crypto.VerifyDecryptable(context.Background(), openGood, cfg); err != nil {
		t.Fatalf("a good bundle failed its own decryptability check: %v", err)
	}

	// A bundle whose key material was corrupted after writing.
	corrupted := append([]byte(nil), sealed...)
	for i := 30; i < 90 && i < len(corrupted); i++ {
		corrupted[i] ^= 0xff
	}
	openBad := func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(corrupted)), nil }
	if err := crypto.VerifyDecryptable(context.Background(), openBad, cfg); err == nil {
		t.Fatal("a corrupted bundle passed the decryptability check")
	}

	// A bundle written in the clear when encryption was requested.
	plain, _ := sealFixture(t, crypto.Config{})
	openPlain := func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(plain)), nil }
	recipientCfg := crypto.Config{Enabled: true, Mode: snapshot.EncryptionRecipients, Recipients: []string{"age1"}}
	if err := crypto.VerifyDecryptable(context.Background(), openPlain, recipientCfg); err == nil {
		t.Fatal("an unencrypted bundle passed a check that encryption was applied")
	}
}

func TestVerifyDecryptable_RecipientsWithoutHoldingAPrivateKey(t *testing.T) {
	_, pub, err := crypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	cfg := crypto.Config{Enabled: true, Mode: snapshot.EncryptionRecipients, Recipients: []string{pub}}
	sealed, _ := sealFixture(t, cfg)

	open := func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(sealed)), nil }
	if err := crypto.VerifyDecryptable(context.Background(), open, cfg); err != nil {
		t.Fatalf("the header check failed on a correctly encrypted bundle: %v", err)
	}
}

// Encryption must not break the streaming guarantee the packager provides.
//
// The claim under test is that memory does not scale with the bundle, not that
// it is small in absolute terms: passphrase mode uses scrypt, which is
// memory-hard on purpose and costs a few hundred megabytes once per seal
// whatever the input size. Measuring the *difference* between a small and a
// large bundle is what actually distinguishes streaming from buffering.
func TestCrypto_BoundedMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates a large staged artifact")
	}
	// Recipient mode is used here rather than passphrase mode so the scrypt
	// cost does not swamp the signal the test is looking for.
	_, pub, err := crypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	cfg := crypto.Config{Enabled: true, Mode: snapshot.EncryptionRecipients, Recipients: []string{pub}}

	small := sealPeak(t, cfg, 4<<20)
	large := sealPeak(t, cfg, 512<<20)

	growth := int64(large) - int64(small)
	const allowance = 64 << 20
	if growth > allowance {
		t.Fatalf("sealing 512 MB peaked %d MB above sealing 4 MB, which is more than the %d MB a streaming pipeline should need",
			growth>>20, allowance>>20)
	}
	t.Logf("peak heap: %d MB for 4 MB, %d MB for 512 MB", small>>20, large>>20)
}

// sealPeak seals a bundle of the given payload size and reports peak heap.
func sealPeak(t *testing.T, cfg crypto.Config, size int) uint64 {
	t.Helper()
	b, err := snapshot.NewBuilder(filepath.Join(t.TempDir(), "stage"))
	if err != nil {
		t.Fatal(err)
	}
	chunk := bytes.Repeat([]byte("keycloak realm export payload\n"), 4096)
	if _, err := b.Stage(context.Background(), "realm/big.json", &repeatReader{chunk: chunk, remaining: size}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Document(snapshot.EnvelopePath, snapshot.Envelope{SchemaVersion: snapshot.SchemaVersion}); err != nil {
		t.Fatal(err)
	}
	sealer, err := crypto.NewSealer(cfg)
	if err != nil {
		t.Fatal(err)
	}

	runtime.GC()
	var peak uint64
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > peak {
				peak = m.HeapAlloc
			}
			time.Sleep(time.Millisecond)
		}
	}()
	if _, err := b.Seal(context.Background(), io.Discard, sealer); err != nil {
		t.Fatal(err)
	}
	close(stop)
	<-done
	return peak
}

type repeatReader struct {
	chunk     []byte
	remaining int
}

func (r *repeatReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := copy(p, r.chunk)
	if n > r.remaining {
		n = r.remaining
	}
	r.remaining -= n
	return n, nil
}

func open(t *testing.T, sealed []byte, opener snapshot.Opener) *snapshot.Opened {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "open")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	o, err := snapshot.Open(context.Background(), bytes.NewReader(sealed),
		snapshot.OpenOptions{Dir: dir, Opener: opener})
	if err != nil {
		t.Fatal(err)
	}
	return o
}
