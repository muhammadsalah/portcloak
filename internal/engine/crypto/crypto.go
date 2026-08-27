// Package crypto encrypts and decrypts sealed snapshot bundles.
//
// Encryption is opt-in (D8). It is offered prominently and recommended, and
// declining it is one deliberate action rather than a default — but declining
// is a respected choice, not a punished one. What PortCloak owes an operator
// who declines is that they can never afterwards say they did not realise the
// file held unmasked secrets and private signing keys in the clear.
package crypto

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"filippo.io/age"

	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/snapshot"
)

// ErrNoRecipients is returned when encryption was asked for with nothing to
// encrypt to.
var ErrNoRecipients = errors.New("encryption needs a passphrase or at least one recipient")

// ErrWrongKey is returned when a bundle cannot be opened with what was given.
var ErrWrongKey = errors.New("this snapshot could not be decrypted with the key supplied")

// Mode is how a bundle is encrypted.
type Mode = snapshot.EncryptionMode

// Config is what the capture wizard collected.
type Config struct {
	Enabled bool
	Mode    Mode
	// Passphrase is used in passphrase mode.
	Passphrase string
	// Recipients are age public keys (age1...), used in recipients mode. Each
	// listed recipient can open the bundle with their own private key, which is
	// what separates "who can capture" from "who can restore".
	Recipients []string
}

// Validate rejects a configuration that would produce a bundle nobody can open.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	switch c.Mode {
	case snapshot.EncryptionPassphrase:
		if strings.TrimSpace(c.Passphrase) == "" {
			return resil.Fatal("check the encryption settings",
				"Encryption is on but no passphrase was given.", ErrNoRecipients).
				WithAdvice("Enter a passphrase, or choose recipients instead.")
		}
	case snapshot.EncryptionRecipients:
		if len(c.Recipients) == 0 {
			return resil.Fatal("check the encryption settings",
				"Encryption is on but no recipients were given.", ErrNoRecipients).
				WithAdvice("Add at least one age public key, or switch to a passphrase.")
		}
		for _, r := range c.Recipients {
			if _, err := age.ParseX25519Recipient(strings.TrimSpace(r)); err != nil {
				return resil.Fatal("check the encryption settings",
					fmt.Sprintf("%q is not an age public key. They start with age1.", short(r)), err)
			}
		}
	case snapshot.EncryptionNone:
		return resil.Fatal("check the encryption settings",
			"Encryption is on but no mode was chosen.", ErrNoRecipients)
	default:
		return resil.Fatal("check the encryption settings",
			fmt.Sprintf("%q is not an encryption mode PortCloak knows.", c.Mode), nil)
	}
	return nil
}

// Sealer encrypts a bundle as it is written.
type Sealer struct {
	recipients []age.Recipient
	describe   snapshot.Encryption
}

// NewSealer builds the encryptor for a configuration. A disabled configuration
// returns nil, which the packager treats as "write in the clear".
func NewSealer(c Config) (*Sealer, error) {
	if !c.Enabled {
		return nil, nil
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}

	s := &Sealer{describe: snapshot.Encryption{Enabled: true, Mode: c.Mode}}
	switch c.Mode {
	case snapshot.EncryptionPassphrase:
		r, err := age.NewScryptRecipient(c.Passphrase)
		if err != nil {
			return nil, resil.Fatal("prepare encryption", "The passphrase could not be used to encrypt.", err)
		}
		// The work factor is age's own, deliberately left alone. scrypt is
		// memory-hard by design, so passphrase mode costs a few hundred
		// megabytes and about a second once per seal and once per open. That is
		// the point of the algorithm, it does not scale with the bundle, and
		// second-guessing a maintained default here would trade an attacker's
		// cost for an operator's convenience.
		s.recipients = []age.Recipient{r}
	case snapshot.EncryptionRecipients:
		for _, raw := range c.Recipients {
			r, err := age.ParseX25519Recipient(strings.TrimSpace(raw))
			if err != nil {
				return nil, resil.Fatal("prepare encryption",
					fmt.Sprintf("%q is not an age public key.", short(raw)), err)
			}
			s.recipients = append(s.recipients, r)
			s.describe.Recipients = append(s.describe.Recipients, strings.TrimSpace(raw))
		}
	}
	return s, nil
}

// Wrap returns a writer whose contents end up encrypted into w.
func (s *Sealer) Wrap(w io.Writer) (io.WriteCloser, error) {
	return age.Encrypt(w, s.recipients...)
}

// Describe is what the envelope records — enough to know whether you can open a
// bundle before you try.
func (s *Sealer) Describe() snapshot.Encryption { return s.describe }

// Opener decrypts a bundle as it is read.
type Opener struct {
	identities []age.Identity
}

// NewPassphraseOpener opens a passphrase-encrypted bundle.
func NewPassphraseOpener(passphrase string) (*Opener, error) {
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, resil.Fatal("open the snapshot", "The passphrase could not be used to decrypt.", err)
	}
	return &Opener{identities: []age.Identity{id}}, nil
}

// NewIdentityOpener opens a recipient-encrypted bundle with one or more age
// private keys.
func NewIdentityOpener(keys []string) (*Opener, error) {
	var ids []age.Identity
	for _, raw := range keys {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		id, err := age.ParseX25519Identity(raw)
		if err != nil {
			return nil, resil.Fatal("open the snapshot",
				"That is not an age private key. They start with AGE-SECRET-KEY-1.", err)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, resil.Fatal("open the snapshot", "No age identity was supplied.", ErrNoRecipients)
	}
	return &Opener{identities: ids}, nil
}

// OpenerFor builds the right opener from what the operator supplied and what
// the envelope says the bundle needs.
func OpenerFor(enc snapshot.Encryption, passphrase string, identities []string) (*Opener, error) {
	if !enc.Enabled {
		return nil, nil
	}
	switch enc.Mode {
	case snapshot.EncryptionPassphrase:
		if passphrase == "" {
			return nil, resil.Fatal("open the snapshot",
				"This snapshot is encrypted with a passphrase, and none was given.", snapshot.ErrEncrypted)
		}
		return NewPassphraseOpener(passphrase)
	case snapshot.EncryptionRecipients:
		if len(identities) == 0 {
			return nil, resil.Fatal("open the snapshot",
				fmt.Sprintf("This snapshot is encrypted to %d recipient(s), and no matching private key was given.", len(enc.Recipients)),
				snapshot.ErrEncrypted)
		}
		return NewIdentityOpener(identities)
	default:
		return nil, resil.Fatal("open the snapshot",
			fmt.Sprintf("This snapshot records an encryption mode PortCloak does not recognise (%q).", enc.Mode), nil)
	}
}

// Unwrap decrypts r.
func (o *Opener) Unwrap(r io.Reader) (io.Reader, error) {
	dec, err := age.Decrypt(r, o.identities...)
	if err != nil {
		// A wrong key is not something to retry. Saying so plainly beats
		// letting a retry loop turn one clear failure into five slow ones.
		return nil, resil.Fatal("decrypt the snapshot",
			"This snapshot could not be decrypted with the key supplied.", errors.Join(ErrWrongKey, err)).
			WithAdvice("Check the passphrase, or that you hold a private key matching one of the recipients recorded in the snapshot.")
	}
	return dec, nil
}

// VerifyDecryptable proves at capture time that the bundle just written can
// actually be opened.
//
// A bundle that cannot be decrypted should be discovered at capture, not
// eighteen months later during an incident. The check reads only the first
// block back, so it costs almost nothing.
func VerifyDecryptable(ctx context.Context, open func() (io.ReadCloser, error), c Config) error {
	if !c.Enabled {
		return nil
	}
	var opener *Opener
	var err error
	switch c.Mode {
	case snapshot.EncryptionPassphrase:
		opener, err = NewPassphraseOpener(c.Passphrase)
	case snapshot.EncryptionRecipients:
		// Recipient mode encrypts to public keys PortCloak does not hold the
		// private half of, so the strongest available check is that the header
		// parses and names the expected number of recipients.
		return verifyHeader(open, len(c.Recipients))
	default:
		return nil
	}
	if err != nil {
		return err
	}

	rc, err := open()
	if err != nil {
		return err
	}
	defer rc.Close() //nolint:errcheck // read-only.

	dec, err := opener.Unwrap(rc)
	if err != nil {
		return err
	}
	buf := make([]byte, 4096)
	if _, err := io.ReadFull(dec, buf); err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return resil.Fatal("verify the snapshot can be decrypted",
			"The snapshot was written but could not be read back. It has not been left in storage.", err)
	}
	return nil
}

// verifyHeader confirms an age header is present and well formed without
// holding a private key.
func verifyHeader(open func() (io.ReadCloser, error), wantRecipients int) error {
	rc, err := open()
	if err != nil {
		return err
	}
	defer rc.Close() //nolint:errcheck // read-only.

	head := make([]byte, 4096)
	n, err := io.ReadFull(rc, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return resil.Fatal("verify the snapshot can be decrypted",
			"The snapshot was written but could not be read back.", err)
	}
	head = head[:n]

	if !bytes.HasPrefix(head, []byte("age-encryption.org/v1\n")) {
		return resil.Fatal("verify the snapshot can be decrypted",
			"The snapshot does not carry an age header, so it was not encrypted as requested.", nil)
	}
	stanzas := bytes.Count(head, []byte("-> X25519 "))
	if stanzas < wantRecipients {
		return resil.Fatal("verify the snapshot can be decrypted",
			fmt.Sprintf("The snapshot was encrypted to %d recipient(s) but %d were requested.", stanzas, wantRecipients), nil)
	}
	return nil
}

// GenerateIdentity produces a fresh age keypair, so an operator without one can
// still choose recipient mode.
func GenerateIdentity() (privateKey, publicKey string, err error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", err
	}
	return id.String(), id.Recipient().String(), nil
}

// IdentityPublicKey derives the recipient from an age private key.
//
// An imported key has to record its public half, because that is what a capture
// seals to and what a snapshot's recipient list is matched against — and asking
// an operator to paste both halves of a keypair they already hold is asking
// them to get it wrong.
func IdentityPublicKey(privateKey string) (string, error) {
	id, err := age.ParseX25519Identity(strings.TrimSpace(privateKey))
	if err != nil {
		return "", resil.Fatal("read the key",
			"That is not an age private key. They start with AGE-SECRET-KEY-1.", err)
	}
	return id.Recipient().String(), nil
}

// ValidRecipient reports whether a string is an age public key.
func ValidRecipient(publicKey string) bool {
	_, err := age.ParseX25519Recipient(strings.TrimSpace(publicKey))
	return err == nil
}

func short(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 16 {
		return s
	}
	return s[:8] + "…" + s[len(s)-4:]
}
