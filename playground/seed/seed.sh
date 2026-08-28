#!/usr/bin/env bash
# Copyright 2026 Muhammad Salah
# SPDX-License-Identifier: Apache-2.0
#
# Fills a running playground with realms worth capturing.
#
#   playground/seed/seed.sh docker      [--ldap-users 5000] [--users 250]
#   playground/seed/seed.sh openshift   [same]
#
# Both do the same three things twice, once for corp-a on kc-a and once for
# corp-b on kc-b:
#
#   1. generate the realm and the LDAP tree from one seed, so the two agree
#   2. load the tree into the directory with ldapadd, inside its own container
#   3. create the realm through the admin API, with the federation provider
#      already pointing at that directory
#
# kc-merged is left empty. It is the restore destination, and a restore into a
# Keycloak that has never seen these users is the case worth rehearsing.
#
# Re-running replaces both halves rather than merging into them: a playground
# that accumulates is a playground whose state nobody can describe.

set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
out="$root/playground/.seed"

mode=${1:-docker}
shift || true

compose="docker compose -f $root/playground/target/docker/compose.yml"

USERS=250
LDAP_USERS=5000
CLIENTS=12
while [ $# -gt 0 ]; do
	case "$1" in
	--users) USERS=$2 && shift 2 ;;
	--ldap-users) LDAP_USERS=$2 && shift 2 ;;
	--clients) CLIENTS=$2 && shift 2 ;;
	*) echo "!! unknown flag: $1" >&2 && exit 2 ;;
	esac
done

command -v go >/dev/null || {
	echo "!! go is not on PATH" >&2
	exit 1
}
mkdir -p "$out"

# generate writes both halves of one realm from one seed.
generate() {
	local realm=$1 seed=$2 ldap_host=$3 ldap_port=$4
	(cd "$here" && go run . realm \
		-realm "$realm" -seed "$seed" \
		-users "$USERS" -clients "$CLIENTS" \
		-ldap-host "$ldap_host" -ldap-port "$ldap_port" \
		-out "$out/$realm-realm.json")
	(cd "$here" && go run . ldif \
		-realm "$realm" -seed "$seed" \
		-ldap-users "$LDAP_USERS" \
		-out "$out/$realm.ldif")
}

# load_directory pipes one realm's LDIF into the directory that will serve it.
#
# ldapadd runs inside the container that already has it, and the file arrives on
# stdin rather than through a bind mount that would have to exist before the
# container started. -c keeps it going past an entry that is already there, so
# re-seeding tops a directory up instead of failing on the first collision.
load_directory() {
	local service=$1 realm=$2
	# Braced because of the ellipsis. macOS ships bash 3.2, which finds the end
	# of a variable name a byte at a time: in "$service…" it takes the first
	# byte of the ellipsis to be part of the name, looks up a variable that was
	# never set, and `set -u` ends the run. Any non-ASCII character straight
	# after an unbraced name does this, and only on the old bash — which is why
	# CI, on bash 5, never saw it.
	echo "loading $realm into ${service}…"
	$compose exec -T "$service" ldapadd -x \
		-H ldap://127.0.0.1:1389 \
		-D "cn=admin,dc=$realm,dc=example,dc=com" -w adminpassword \
		-c <"$out/$realm.ldif" || true
}

# load_directory_oc is the same thing where the directory is a pod.
load_directory_oc() {
	local app=$1 realm=$2 pod
	echo "loading $realm into ${app}…"
	pod=$(oc get pod -n kc -l "app=$app" -o jsonpath='{.items[0].metadata.name}')
	oc exec -n kc -i "$pod" -- ldapadd -x \
		-H ldap://127.0.0.1:1389 \
		-D "cn=admin,dc=$realm,dc=example,dc=com" -w adminpassword \
		-c <"$out/$realm.ldif" || true
}

# post sends a generated realm to a Keycloak, replacing any realm of that name.
post() {
	local realm=$1 url=$2
	(cd "$here" && go run . realm \
		-realm "$realm" -seed "$3" \
		-users "$USERS" -clients "$CLIENTS" \
		-ldap-host "$4" -ldap-port "$5" \
		-apply "$url" -admin "admin:admin")
}

case "$mode" in
docker)
	# The output is what is tested, not the exit status. `compose ps` succeeds
	# whether or not anything is running — an empty playground is a valid answer
	# to the question, not an error — so testing the status let a stopped stack
	# through to fail later as "service is not running" and a refused connection,
	# which is a longer way of saying what this message says.
	[ -n "$($compose ps --status running --quiet 2>/dev/null)" ] || {
		echo "!! the docker playground is not up. Start it with:" >&2
		echo "   $compose up -d" >&2
		exit 1
	}

	generate corp-a 1 ldap-a 1389
	generate corp-b 2 ldap-b 1389

	load_directory ldap-a corp-a
	load_directory ldap-b corp-b

	post corp-a http://127.0.0.1:8080 1 ldap-a 1389
	post corp-b http://127.0.0.1:8081 2 ldap-b 1389
	;;

openshift)
	command -v oc >/dev/null || {
		echo "!! oc is not on PATH" >&2
		exit 1
	}

	generate corp-a 1 ldap-a 1389
	generate corp-b 2 ldap-b 1389

	load_directory_oc ldap-a corp-a
	load_directory_oc ldap-b corp-b

	kc_a=$(oc get route kc-a -n kc -o jsonpath='{.spec.host}')
	kc_b=$(oc get route kc-b -n kc -o jsonpath='{.spec.host}')
	post corp-a "http://$kc_a" 1 ldap-a 1389
	post corp-b "http://$kc_b" 2 ldap-b 1389
	;;

*)
	echo "usage: $0 [docker|openshift] [--users N] [--ldap-users N] [--clients N]" >&2
	exit 2
	;;
esac

echo
echo "Seeded. The generated documents are in playground/.seed — the realm JSON is"
echo "what kc.sh import would read, and the LDIF is what the directory holds."
