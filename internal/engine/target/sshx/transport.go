// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Package sshx is the shared SSH transport behind both the SSH target and the
// SFTP storage backend.
//
// They share it deliberately: an operator who has already told PortCloak how to
// reach a host should not have to describe the same host twice, and the same
// keychain entry can back both.
package sshx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/resil"
)

// HostKeyDecision is what an operator was asked about an unknown host key.
type HostKeyDecision string

const (
	// HostKeyStrict refuses an unknown host, which is the default.
	HostKeyStrict HostKeyDecision = "strict"
	// HostKeyAccept records that the operator accepted this key.
	HostKeyAccept HostKeyDecision = "accept"
)

// UnknownHostKeyError is returned on a first connection to a host PortCloak has
// never seen.
//
// It is a distinct error because the answer is a decision, not a fix: the
// operator has to look at the fingerprint and say yes. Silently accepting would
// remove the only protection SSH offers against a man in the middle.
type UnknownHostKeyError struct {
	Host        string
	Fingerprint string
	KeyType     string
}

func (e *UnknownHostKeyError) Error() string {
	return fmt.Sprintf("%s is not in this machine's known_hosts. Its %s key fingerprint is %s — check that against the host before accepting it.",
		e.Host, e.KeyType, e.Fingerprint)
}

// Endpoint describes one hop.
type Endpoint struct {
	Host string
	Port int
	User string
	Auth config.SSHAuth
	// Secret is the resolved private key or password. It is never logged and
	// never written anywhere.
	Secret string
	// Passphrase decrypts an encrypted private key.
	Passphrase string
}

// Address renders host:port.
func (e Endpoint) Address() string {
	port := e.Port
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(e.Host, fmt.Sprint(port))
}

// Config is everything needed to open a connection, including an optional jump
// host in front of it.
type Config struct {
	Target Endpoint
	Jump   *Endpoint
	// KnownHostsFile defaults to ~/.ssh/known_hosts.
	KnownHostsFile string
	// AcceptNewHostKey records the operator's decision for this connection.
	AcceptNewHostKey bool
	// Timeout bounds the handshake.
	Timeout time.Duration
	// AgentSocket overrides SSH_AUTH_SOCK, for tests.
	AgentSocket string
}

// Conn is an open SSH connection, with the jump host it was reached through.
type Conn struct {
	client *ssh.Client
	jump   *ssh.Client

	mu     sync.Mutex
	closed bool
	addr   string
}

// Client is the underlying SSH client, for SFTP.
func (c *Conn) Client() *ssh.Client { return c.client }

// Address is what this connection reached.
func (c *Conn) Address() string { return c.addr }

// Close shuts the connection and its jump host down.
func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true

	var errs []error
	if c.client != nil {
		if err := c.client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
	}
	if c.jump != nil {
		if err := c.jump.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Dial opens a connection, through a jump host when one is configured.
func Dial(ctx context.Context, cfg Config) (*Conn, error) {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	if cfg.Jump == nil {
		client, err := dialDirect(ctx, cfg, cfg.Target, timeout)
		if err != nil {
			return nil, err
		}
		return &Conn{client: client, addr: cfg.Target.Address()}, nil
	}

	// The chain is established end to end, so a jump host that works and a
	// target that does not are told apart in the message.
	jumpClient, err := dialDirect(ctx, cfg, *cfg.Jump, timeout)
	if err != nil {
		return nil, wrapDial(err, "the jump host "+cfg.Jump.Address())
	}

	conn, err := jumpClient.DialContext(ctx, "tcp", cfg.Target.Address())
	if err != nil {
		_ = jumpClient.Close()
		return nil, resil.Retry("connect through the jump host",
			fmt.Sprintf("%s is reachable but could not open a connection onward to %s.", cfg.Jump.Address(), cfg.Target.Address()), err)
	}

	clientCfg, err := clientConfig(cfg, cfg.Target, timeout)
	if err != nil {
		_ = jumpClient.Close()
		return nil, err
	}
	ncc, chans, reqs, err := ssh.NewClientConn(conn, cfg.Target.Address(), clientCfg)
	if err != nil {
		_ = jumpClient.Close()
		return nil, wrapDial(err, cfg.Target.Address())
	}
	return &Conn{
		client: ssh.NewClient(ncc, chans, reqs),
		jump:   jumpClient,
		addr:   cfg.Target.Address(),
	}, nil
}

func dialDirect(ctx context.Context, cfg Config, ep Endpoint, timeout time.Duration) (*ssh.Client, error) {
	clientCfg, err := clientConfig(cfg, ep, timeout)
	if err != nil {
		return nil, err
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", ep.Address())
	if err != nil {
		return nil, wrapDial(err, ep.Address())
	}
	ncc, chans, reqs, err := ssh.NewClientConn(conn, ep.Address(), clientCfg)
	if err != nil {
		_ = conn.Close()
		return nil, wrapDial(err, ep.Address())
	}
	return ssh.NewClient(ncc, chans, reqs), nil
}

// wrapDial turns a transport error into the sentence and the retry decision an
// operator needs.
func wrapDial(err error, where string) error {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "unable to authenticate"),
		strings.Contains(msg, "permission denied"),
		strings.Contains(msg, "no supported methods"):
		// Authentication is never retried. Retrying a rejected credential
		// wastes a minute and buries a message the operator needs now.
		return resil.Fatal("connect over SSH",
			fmt.Sprintf("%s refused the credentials PortCloak offered.", where), err).
			WithAdvice("Check the user, the auth method, and that the credential in this machine's keychain is the right one.")
	case strings.Contains(msg, "knownhosts"), strings.Contains(msg, "host key"):
		return resil.Fatal("connect over SSH",
			fmt.Sprintf("The host key %s presented does not match what this machine has recorded for it.", where), err).
			WithAdvice("If the host was genuinely rebuilt, remove its entry from known_hosts. If it was not, stop and investigate.")
	default:
		return resil.Retry("connect over SSH",
			fmt.Sprintf("PortCloak could not reach %s.", where), err)
	}
}

func clientConfig(cfg Config, ep Endpoint, timeout time.Duration) (*ssh.ClientConfig, error) {
	auth, err := authMethods(cfg, ep)
	if err != nil {
		return nil, err
	}
	callback, err := hostKeyCallback(cfg, ep)
	if err != nil {
		return nil, err
	}
	return &ssh.ClientConfig{
		User:            ep.User,
		Auth:            auth,
		HostKeyCallback: callback,
		Timeout:         timeout,
	}, nil
}

func authMethods(cfg Config, ep Endpoint) ([]ssh.AuthMethod, error) {
	switch ep.Auth {
	case config.SSHKey:
		if ep.Secret == "" {
			return nil, resil.Fatal("connect over SSH",
				fmt.Sprintf("No private key was available for %s.", ep.Address()), nil).
				WithAdvice("The key lives in this machine's keychain. If the configuration was copied from another machine, enter it again here.")
		}
		signer, err := parseKey([]byte(ep.Secret), ep.Passphrase)
		if err != nil {
			return nil, err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil

	case config.SSHPassword:
		if ep.Secret == "" {
			return nil, resil.Fatal("connect over SSH",
				fmt.Sprintf("No password was available for %s.", ep.Address()), nil)
		}
		return []ssh.AuthMethod{ssh.Password(ep.Secret)}, nil

	case config.SSHAgent, "":
		// Agent auth stores no secret at all, which is why it is worth
		// supporting rather than insisting on a key PortCloak has to hold.
		socket := cfg.AgentSocket
		if socket == "" {
			socket = os.Getenv("SSH_AUTH_SOCK")
		}
		if socket == "" {
			return nil, resil.Fatal("connect over SSH",
				"This environment is set to use the SSH agent, but no agent is running for this session.", nil).
				WithAdvice("Start an agent and add the key, or switch the environment to a private key.")
		}
		conn, err := net.Dial("unix", socket)
		if err != nil {
			return nil, resil.Fatal("connect over SSH",
				"PortCloak could not talk to the SSH agent.", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeysCallback(agent.NewClient(conn).Signers)}, nil

	default:
		return nil, resil.Fatal("connect over SSH",
			fmt.Sprintf("%q is not an SSH auth method PortCloak knows.", ep.Auth), nil)
	}
}

func parseKey(pem []byte, passphrase string) (ssh.Signer, error) {
	if passphrase != "" {
		signer, err := ssh.ParsePrivateKeyWithPassphrase(pem, []byte(passphrase))
		if err != nil {
			return nil, resil.Fatal("connect over SSH",
				"The private key could not be decrypted with the passphrase given.", err)
		}
		return signer, nil
	}
	signer, err := ssh.ParsePrivateKey(pem)
	if err != nil {
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return nil, resil.Fatal("connect over SSH",
				"This private key is encrypted and no passphrase was supplied.", err).
				WithAdvice("Add the passphrase to the environment, or use an unencrypted key or the SSH agent.")
		}
		return nil, resil.Fatal("connect over SSH",
			"PortCloak could not read that private key.", err)
	}
	return signer, nil
}

// hostKeyCallback verifies against known_hosts, and surfaces a first connection
// as a decision rather than accepting it silently.
func hostKeyCallback(cfg Config, ep Endpoint) (ssh.HostKeyCallback, error) {
	path := cfg.KnownHostsFile
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".ssh", "known_hosts")
	}

	known, err := knownhosts.New(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, resil.Fatal("connect over SSH",
				fmt.Sprintf("PortCloak could not read %s.", path), err)
		}
		// No known_hosts at all: every host is a first connection.
		known = func(string, net.Addr, ssh.PublicKey) error {
			return &knownhosts.KeyError{}
		}
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := known(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			// Unknown host, as opposed to a mismatch. A mismatch always fails.
			if cfg.AcceptNewHostKey {
				return appendKnownHost(path, hostname, key)
			}
			return &UnknownHostKeyError{
				Host:        hostname,
				Fingerprint: ssh.FingerprintSHA256(key),
				KeyType:     key.Type(),
			}
		}
		return err
	}, nil
}

func appendKnownHost(path, hostname string, key ssh.PublicKey) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // the write below is what matters.
	line := knownhosts.Line([]string{hostname}, key)
	_, err = f.WriteString(line + "\n")
	return err
}

// FromEnvironment builds a transport configuration from an SSH environment.
func FromEnvironment(env config.Environment, creds config.CredentialStore) (Config, error) {
	secret, err := config.Resolve(creds, env.CredentialRef, env.Name)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Target: Endpoint{
			Host: env.Host, Port: env.Port, User: env.User,
			Auth: env.Auth, Secret: secret,
		},
	}
	if env.JumpHost != nil {
		jumpSecret, err := config.Resolve(creds, env.JumpHost.CredentialRef, env.Name+" (jump host)")
		if err != nil {
			return Config{}, err
		}
		cfg.Jump = &Endpoint{
			Host: env.JumpHost.Host, Port: env.JumpHost.Port, User: env.JumpHost.User,
			Auth: env.JumpHost.Auth, Secret: jumpSecret,
		}
		if cfg.Jump.User == "" {
			cfg.Jump.User = env.User
		}
	}
	return cfg, nil
}

// FromStorage builds a transport configuration from an SSH storage definition.
func FromStorage(st config.Storage, creds config.CredentialStore) (Config, error) {
	secret, err := config.Resolve(creds, st.CredentialRef, st.Name)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Target: Endpoint{
			Host: st.Host, Port: st.Port, User: st.User,
			Auth: st.Auth, Secret: secret,
		},
	}
	if st.JumpHost != nil {
		jumpSecret, err := config.Resolve(creds, st.JumpHost.CredentialRef, st.Name+" (jump host)")
		if err != nil {
			return Config{}, err
		}
		cfg.Jump = &Endpoint{
			Host: st.JumpHost.Host, Port: st.JumpHost.Port, User: st.JumpHost.User,
			Auth: st.JumpHost.Auth, Secret: jumpSecret,
		}
		if cfg.Jump.User == "" {
			cfg.Jump.User = st.User
		}
	}
	return cfg, nil
}
