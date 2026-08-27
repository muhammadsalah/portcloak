package admin

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"portcloak/internal/engine/resil"
)

// statusError carries a response code so it can be classified and recognised.
type statusError struct {
	Status int
	Body   string
	Path   string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("%s returned %d: %s", e.Path, e.Status, truncate(e.Body, 300))
}

func isNotFound(err error) bool {
	var se *statusError
	return errors.As(err, &se) && se.Status == http.StatusNotFound
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// token_ obtains and caches an access token.
//
// The trailing underscore keeps it out of the way of the cached field; this is
// the one place in the package that handles a credential, and it never logs it.
func (c *Client) token_(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.token != "" && time.Now().Before(c.expires) {
		t := c.token
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()

	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("client_id", c.clientID)
	form.Set("username", c.username)
	form.Set("password", c.password)

	endpoint := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", c.base, url.PathEscape(c.authRealm))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", c.classifyTransport("reach the Admin API",
			fmt.Sprintf("PortCloak could not reach the Admin API at %s.", c.base), err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return "", resil.Fatal("authenticate to the Admin API",
				"The Admin API rejected the credentials on this environment.", nil).
				WithAdvice("Check the admin user and that its credential in this machine's keychain is current.")
		}
		return "", classifyStatus(resp.StatusCode, string(body), "the token endpoint")
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return "", resil.Fatal("authenticate to the Admin API",
			"The Admin API returned something that is not a token.", err)
	}

	ttl := time.Duration(tok.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Minute
	}
	c.mu.Lock()
	c.token = tok.AccessToken
	// Renewed a little early, so a long capture does not fail on a token that
	// expired between two calls.
	c.expires = time.Now().Add(ttl - 10*time.Second)
	c.mu.Unlock()
	return tok.AccessToken, nil
}

func classifyStatus(code int, body, where string) error {
	err := &statusError{Status: code, Body: body, Path: where}
	if resil.ClassifyHTTPStatus(code) == resil.Retryable {
		return resil.Retry("talk to the Admin API",
			fmt.Sprintf("%s returned %d.", where, code), err)
	}
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return resil.Fatal("talk to the Admin API",
			fmt.Sprintf("The Admin API refused this request: %s returned %d.", where, code), err).
			WithAdvice("The account may not have the realm-management role the operation needs.")
	case http.StatusNotFound:
		return resil.Fatal("talk to the Admin API",
			fmt.Sprintf("%s was not found on this server.", where), err)
	}
	return resil.Fatal("talk to the Admin API", err.Error(), err)
}

func (c *Client) get(ctx context.Context, p string, into any) error {
	return c.do(ctx, http.MethodGet, p, nil, into)
}

func (c *Client) post(ctx context.Context, p string, body []byte, into any) error {
	return c.do(ctx, http.MethodPost, p, body, into)
}

func (c *Client) do(ctx context.Context, method, p string, body []byte, into any) error {
	token, err := c.token_(ctx)
	if err != nil {
		return err
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+p, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return c.classifyTransport("talk to the Admin API",
			fmt.Sprintf("The connection to %s dropped.", c.base), err)
	}
	defer resp.Body.Close() //nolint:errcheck

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode >= 400 {
		return classifyStatus(resp.StatusCode, string(raw), p)
	}
	if readErr != nil {
		return resil.Retry("talk to the Admin API",
			"The response from the Admin API ended unexpectedly.", readErr)
	}
	if into == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return resil.Fatal("talk to the Admin API",
			fmt.Sprintf("The Admin API returned something PortCloak could not read from %s.", p), err)
	}
	return nil
}

// classifyTransport turns a failed request into the right kind of failure.
//
// Everything at this level used to be reported as retryable, which is right for
// a dropped connection and wrong for a certificate: a Keycloak behind a
// self-signed or private-CA certificate is not going to become trusted on the
// fourth attempt, so the retry budget was being spent proving that. Worse, the
// message said only that the server could not be reached — the operator was
// looking at a working URL, being told it was unreachable, with no mention of a
// certificate and no mention of the setting that accepts one.
func (c *Client) classifyTransport(op, message string, err error) error {
	if certProblem(err) {
		return resil.Fatal(op,
			fmt.Sprintf("The TLS certificate presented by %s is not trusted by this machine: %s", c.base, certReason(err)), err).
			WithAdvice("If this is a self-signed or private-CA certificate you recognise, turn on " +
				"“Accept a self-signed certificate” on this environment. It applies to the Admin API " +
				"only, and never to a snapshot's contents.")
	}
	return resil.Retry(op, message, err).
		WithAdvice("Verification and dependency detection are optional; a capture succeeds without them.")
}

// certProblem reports whether a request failed because the certificate could
// not be verified, as opposed to the connection failing.
func certProblem(err error) bool {
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var verification *tls.CertificateVerificationError
	return errors.As(err, &unknown) || errors.As(err, &hostname) ||
		errors.As(err, &invalid) || errors.As(err, &verification)
}

// certReason states which way the certificate failed, because "self-signed",
// "expired" and "wrong hostname" have three different answers and only one of
// them is the setting this package offers.
func certReason(err error) string {
	var hostname x509.HostnameError
	if errors.As(err, &hostname) {
		return fmt.Sprintf("it is not valid for that host name (%s).", hostname.Host)
	}
	var invalid x509.CertificateInvalidError
	if errors.As(err, &invalid) && invalid.Reason == x509.Expired {
		return "it has expired, or is not valid yet."
	}
	var unknown x509.UnknownAuthorityError
	if errors.As(err, &unknown) {
		return "it was signed by an authority this machine does not know — which is what a self-signed or private-CA certificate looks like."
	}
	return "the certificate could not be verified."
}

// insecureTransport is used only when an environment explicitly asks for it,
// which is a real need on an internal server with a private CA — and a decision
// the operator makes per environment rather than a global default.
func insecureTransport() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in per environment.
	return t
}
