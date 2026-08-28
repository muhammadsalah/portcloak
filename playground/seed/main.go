// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Command seed fills a playground Keycloak and its directory with a realm worth
// testing against.
//
// The realms PortCloak has trouble with are not big; they are *various*. A
// hundred thousand identical users exercise one code path a hundred thousand
// times. What finds faults is a realm where some users have OTP and some have
// passkeys and some have both, where a group is nested four deep, where a
// client has a protocol mapper nobody remembers adding, and where an attribute
// contains a character somebody assumed would never appear in one. So the
// generator's job is variety, and the counts are only the second axis.
//
// Everything it produces is deterministic from --seed. A fidelity test that
// cannot be re-run against the same realm is a test whose failure cannot be
// investigated, and "it worked when I generated it again" is not an answer.
//
// Three subcommands, because the two halves of a federated realm are seeded
// through different doors:
//
//	realm    write a realm JSON, or POST it into a running Keycloak
//	ldif     write the LDAP tree the realm's federation provider will read
//	all      both, plus the federation component that ties them together
//
// Usage examples are in playground/README.md.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "realm":
		err = realmCommand(os.Args[2:])
	case "ldif":
		err = ldifCommand(os.Args[2:])
	case "all":
		err = allCommand(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "!! %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `seed — fills a playground Keycloak with a realm worth testing against

  seed realm  [flags]   a realm: clients, groups, roles, mappers, flows, users
  seed ldif   [flags]   the LDAP tree a federated realm reads its users from
  seed all    [flags]   both, and the federation component joining them

Run any subcommand with -h for its flags.
`)
}

// shape is what every subcommand needs to know about the realm being made. It
// is one struct so the three of them cannot disagree about what "corp-a" means.
type shape struct {
	realm     string
	baseDN    string
	seed      int64
	users     int
	ldapUsers int
	clients   int
	groups    int
	roles     int
}

func (s *shape) flags(fs *flag.FlagSet) {
	fs.StringVar(&s.realm, "realm", "corp-a", "realm name; also names the LDAP suffix")
	fs.StringVar(&s.baseDN, "base-dn", "", "LDAP base DN (default dc=<realm>,dc=example,dc=com)")
	fs.Int64Var(&s.seed, "seed", 1, "makes the output reproducible; the same seed is the same realm")
	fs.IntVar(&s.users, "users", 250, "users written into the realm itself, with credentials")
	fs.IntVar(&s.ldapUsers, "ldap-users", 5000, "users written into the directory, federated in")
	fs.IntVar(&s.clients, "clients", 12, "clients, each with its own scopes, mappers and roles")
	fs.IntVar(&s.groups, "groups", 8, "top-level groups; each grows a subtree")
	fs.IntVar(&s.roles, "roles", 15, "realm roles, some composite")
}

func (s *shape) resolve() {
	if s.baseDN == "" {
		s.baseDN = fmt.Sprintf("dc=%s,dc=example,dc=com", s.realm)
	}
}

// dcOf pulls the first dc= component out of a base DN, which is what an LDIF
// needs for the root entry's dc attribute.
func dcOf(baseDN string) string {
	for _, part := range strings.Split(baseDN, ",") {
		part = strings.TrimSpace(part)
		if after, ok := strings.CutPrefix(part, "dc="); ok {
			return after
		}
	}
	return "example"
}
