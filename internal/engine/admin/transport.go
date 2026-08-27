package admin

import (
	"bytes"
	"context"
	"crypto/tls"
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
		return "", resil.Retry("reach the Admin API",
			fmt.Sprintf("PortCloak could not reach the Admin API at %s.", c.base), err).
			WithAdvice("Verification and dependency detection are optional; a capture succeeds without them.")
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
		return resil.Retry("talk to the Admin API",
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

// insecureTransport is used only when an environment explicitly asks for it,
// which is a real need on an internal server with a private CA — and a decision
// the operator makes per environment rather than a global default.
func insecureTransport() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in per environment.
	return t
}
