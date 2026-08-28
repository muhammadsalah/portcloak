// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"encoding/base64"
	"flag"
	"fmt"
	mathrand "math/rand"
	"os"
	"strings"
)

// ldifCommand writes the directory half of a federated realm.
//
// LDIF rather than a protocol client: the file is applied with ldapadd inside
// the container that already has it, which keeps this module free of an LDAP
// dependency and — more usefully — leaves a text file an operator can read,
// diff and re-apply when a directory needs rebuilding.
func ldifCommand(args []string) error {
	fs := flag.NewFlagSet("ldif", flag.ExitOnError)
	var s shape
	s.flags(fs)
	out := fs.String("out", "", "write the LDIF here (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s.resolve()

	w := os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close() //nolint:errcheck
		w = f
	}

	buf := bufio.NewWriter(w)
	defer buf.Flush() //nolint:errcheck

	if err := writeLDIF(buf, s); err != nil {
		return err
	}
	if *out != "" {
		fmt.Fprintf(os.Stderr, "wrote %s — %d entries under %s\n", *out, s.ldapUsers, s.baseDN)
	}
	return nil
}

func writeLDIF(w *bufio.Writer, s shape) error {
	r := mathrand.New(mathrand.NewSource(s.seed))

	// The suffix itself, then the two containers everything else hangs from.
	// The image is started with LDAP_SKIP_DEFAULT_TREE so this file owns the
	// whole tree rather than adding to one somebody else decided.
	entry(w, "dn: "+s.baseDN,
		"objectClass: dcObject",
		"objectClass: organization",
		"dc: "+dcOf(s.baseDN),
		"o: "+s.realm)

	entry(w, "dn: ou=people,"+s.baseDN,
		"objectClass: organizationalUnit",
		"ou: people")

	entry(w, "dn: ou=groups,"+s.baseDN,
		"objectClass: organizationalUnit",
		"ou: groups")

	// Groups first, so a member reference always points at something that
	// exists by the time ldapadd reaches it. groupOfNames requires at least one
	// member, which is why each starts with the admin DN and gains people
	// afterwards.
	groups := make([][]string, len(departments))
	crowd := people(r, s.ldapUsers, nil, nil)

	for i, p := range crowd {
		g := i % len(departments)
		groups[g] = append(groups[g], fmt.Sprintf("uid=%s,ou=people,%s", escapeRDN(p.Username), s.baseDN))
	}

	for i, dept := range departments {
		lines := []string{
			"objectClass: groupOfNames",
			"cn: " + strings.ToLower(dept),
			"description: " + dept + " department",
			"member: cn=admin," + s.baseDN,
		}
		for _, dn := range groups[i] {
			lines = append(lines, "member: "+dn)
		}
		entry(w, fmt.Sprintf("dn: cn=%s,ou=groups,%s", strings.ToLower(dept), s.baseDN), lines...)
	}

	for _, p := range crowd {
		lines := []string{
			"objectClass: inetOrgPerson",
			"objectClass: organizationalPerson",
			"objectClass: person",
			"objectClass: top",
			"uid: " + p.Username,
			attr("cn", p.First+" "+p.Last),
			attr("sn", p.Last),
			attr("givenName", p.First),
			"mail: " + p.Email,
			"employeeNumber: " + p.Employee,
			attr("l", p.Location),
			attr("departmentNumber", p.Dept),
			attr("title", p.Dept+" engineer"),
			// Plain, and deliberately: OpenLDAP hashes on write when configured
			// to, the directory is on a laptop, and a hash here would only hide
			// what the password is from the person who has to log in with it.
			"userPassword: " + p.Username + "-secret",
		}
		entry(w, fmt.Sprintf("dn: uid=%s,ou=people,%s", escapeRDN(p.Username), s.baseDN), lines...)
	}
	return w.Flush()
}

// escapeRDN makes a value safe to sit inside a distinguished name.
//
// The generator deliberately produces usernames with a quote, a backslash, a
// plus and a space in them, because those are the ones that have broken
// something somewhere. Unescaped, each of them ends the RDN early and produces
// an LDIF that ldapadd rejects — at line 40,000, after the first 39,999 entries
// have already been added, which is the worst place to find out.
//
// RFC 4514: the specials are , + " \ < > ; and #, plus a space at either end.
func escapeRDN(value string) string {
	var b strings.Builder
	for i, r := range value {
		switch {
		case r == ',' || r == '+' || r == '"' || r == '\\' || r == '<' || r == '>' || r == ';' || r == '=':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == ' ' && (i == 0 || i == len(value)-1):
			b.WriteString("\\ ")
		case r == '#' && i == 0:
			b.WriteString("\\#")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// entry writes one LDIF record and the blank line that ends it.
func entry(w *bufio.Writer, dn string, lines ...string) {
	// The DN is written as it is. It is already escaped per RFC 4514, and a DN
	// carrying a character outside ASCII is legal — LDIF only requires base64
	// where the line would otherwise be ambiguous, which an escaped DN is not.
	fmt.Fprintln(w, dn)
	for _, l := range lines {
		fmt.Fprintln(w, ldifLine(l))
	}
	fmt.Fprintln(w)
}

// attr renders one attribute, base64-encoding the value where LDIF requires it.
//
// A value that begins with a space or a colon, or that carries anything outside
// ASCII, has to be written as `name:: base64` — and the generator produces all
// three on purpose, because a directory of Smiths proves nothing about a
// directory with a Dvořák in it.
func attr(name, value string) string {
	if needsBase64(value) {
		return name + ":: " + base64.StdEncoding.EncodeToString([]byte(value))
	}
	return name + ": " + value
}

func needsBase64(value string) bool {
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, " ") || strings.HasPrefix(value, ":") || strings.HasPrefix(value, "<") {
		return true
	}
	if strings.HasSuffix(value, " ") {
		return true
	}
	for _, r := range value {
		if r > 127 || r == '\n' || r == '\r' || r == 0 {
			return true
		}
	}
	return false
}

// ldifLine folds a line that is already in `name: value` form, if the value
// itself needs encoding. Lines built by attr are already correct; the fixed
// ones above are checked here so a generated username with a diacritic cannot
// produce an LDIF that ldapadd rejects at line 40,000.
func ldifLine(line string) string {
	name, value, ok := strings.Cut(line, ": ")
	if !ok || strings.HasSuffix(name, ":") {
		return line
	}
	if needsBase64(value) {
		return name + ":: " + base64.StdEncoding.EncodeToString([]byte(value))
	}
	return line
}
