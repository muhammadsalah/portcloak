#!/usr/bin/env bash
# Copyright 2026 Muhammad Salah
# SPDX-License-Identifier: Apache-2.0
#
# Puts the playground into whatever cluster oc is pointed at, and grants
# PortCloak the permissions its Kubernetes adapter needs.
#
#   playground/target/openshift/apply.sh          # namespace, workloads, RBAC
#   playground/target/openshift/apply.sh urls     # the routes, once they exist
#   playground/target/openshift/apply.sh delete
#
# It refuses to run against a cluster that is not CRC unless PLAYGROUND_ANY_CLUSTER
# is set. Everything here is named for a playground and none of it is
# production-shaped; applying it to a real cluster by having the wrong context
# selected is a mistake worth one guard.

set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)

need() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "!! $1 is not on PATH." >&2
		exit 1
	}
}
need oc

# ingress_ip is the address the routes' nip.io hostnames embed.
#
# CRC's own domain, apps-crc.testing, resolves only because CRC installs a
# resolver for it; a host that has not run `crc setup`, or has had it undone,
# gets nothing. nip.io needs no local configuration at all — it resolves any
# name ending in an embedded address to that address — so the routes work
# wherever the cluster is reachable from.
#
# On macOS `crc ip` is 127.0.0.1, because CRC forwards 80 and 443 into the VM
# rather than exposing it; on Linux it is the VM's own address. Both are right
# for the machine they are read on, which is why this is read rather than
# written down.
ingress_ip() {
	if [ -n "${PLAYGROUND_INGRESS_IP:-}" ]; then
		printf '%s' "$PLAYGROUND_INGRESS_IP"
		return
	fi
	if command -v crc >/dev/null 2>&1 && crc ip >/dev/null 2>&1; then
		crc ip
		return
	fi
	echo "!! Could not read the cluster's address from crc." >&2
	echo "   Set PLAYGROUND_INGRESS_IP to the address the ingress answers on." >&2
	exit 1
}

# render prints the manifests with the address substituted in. The files carry a
# placeholder rather than an address so that nothing in the repository claims to
# know where somebody else's cluster lives.
render() {
	local ip
	ip=$(ingress_ip)
	local f
	for f in "$here"/manifests/*.yaml; do
		sed "s/__INGRESS_IP__/$ip/g" "$f"
		echo "---"
	done
}

guard_cluster() {
	local server
	server=$(oc whoami --show-server 2>/dev/null || true)
	if [ -z "$server" ]; then
		echo "!! oc is not logged in. Run: eval \$(crc oc-env) && crc console --credentials" >&2
		exit 1
	fi
	if [ -n "${PLAYGROUND_ANY_CLUSTER:-}" ]; then
		return
	fi
	case "$server" in
	*crc.testing*) ;;
	*)
		echo "!! $server is not a CRC cluster." >&2
		echo "   Everything here is playground-shaped and named for one." >&2
		echo "   Set PLAYGROUND_ANY_CLUSTER=1 if you meant it." >&2
		exit 1
		;;
	esac
}

case "${1:-apply}" in
apply)
	guard_cluster
	render | oc apply -f -

	# What PortCloak's Kubernetes adapter needs, and no more: it reads the
	# workload it is capturing, creates one clone pod, execs into it, streams a
	# directory out, and deletes it. The role is here rather than in the
	# manifests directory because it is about the tool rather than about the
	# playground, and because an operator reading it should see the whole list
	# in one place.
	oc apply -f - <<-'YAML'
		apiVersion: rbac.authorization.k8s.io/v1
		kind: Role
		metadata:
		  name: portcloak
		  namespace: kc
		rules:
		  - apiGroups: [""]
		    resources: ["pods"]
		    verbs: ["get", "list", "create", "delete"]
		  - apiGroups: [""]
		    resources: ["pods/exec"]
		    verbs: ["create"]
		  - apiGroups: [""]
		    resources: ["pods/log"]
		    verbs: ["get"]
		  - apiGroups: ["apps"]
		    resources: ["deployments", "statefulsets"]
		    verbs: ["get", "list"]
		---
		apiVersion: rbac.authorization.k8s.io/v1
		kind: RoleBinding
		metadata:
		  name: portcloak
		  namespace: kc
		roleRef:
		  apiGroup: rbac.authorization.k8s.io
		  kind: Role
		  name: portcloak
		subjects:
		  - kind: User
		    name: kubeadmin
		    apiGroup: rbac.authorization.k8s.io
		---
		# The directories run under anyuid. The OpenLDAP image starts as its own
		# uid 1001 and writes into paths that uid owns, and restricted-v2 admits
		# a pod as an arbitrary uid from the namespace's range instead — under
		# which the image cannot run its own setup and the pod crash-loops.
		#
		# This is the same grant `oc adm policy add-scc-to-user anyuid -z default`
		# makes, written as the RoleBinding it creates so that it applies with
		# everything else, is idempotent, and is visible in the repository rather
		# than being a change somebody made to a cluster once and remembered.
		#
		# It is a real exception and it is scoped to one namespace of a throwaway
		# cluster. A production deployment of a directory would use an image built
		# to run as any uid; this playground uses the image the Docker half uses,
		# because a playground that differs from itself proves less.
		apiVersion: rbac.authorization.k8s.io/v1
		kind: RoleBinding
		metadata:
		  name: ldap-anyuid
		  namespace: kc
		roleRef:
		  apiGroup: rbac.authorization.k8s.io
		  kind: ClusterRole
		  name: system:openshift:scc:anyuid
		subjects:
		  - kind: ServiceAccount
		    name: default
		    namespace: kc
	YAML

	echo
	echo "Waiting for the three servers. First start builds the schema, which is slow."
	for app in kc-a kc-b kc-merged; do
		oc rollout status "deployment/$app" -n kc --timeout=15m
	done
	"$0" urls
	;;

urls)
	guard_cluster
	echo
	for app in kc-a kc-b kc-merged; do
		host=$(oc get route "$app" -n kc -o jsonpath='{.spec.host}' 2>/dev/null || true)
		[ -n "$host" ] && printf '  %-10s http://%s   admin / admin\n' "$app" "$host"
	done
	echo
	echo "Seed them with: playground/seed/seed.sh openshift"
	;;

delete)
	guard_cluster
	render | oc delete -f - --ignore-not-found
	;;

*)
	echo "usage: $0 [apply|urls|delete]" >&2
	exit 2
	;;
esac
