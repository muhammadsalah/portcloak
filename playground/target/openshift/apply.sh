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
	oc apply -f "$here/manifests/"

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
	oc delete -f "$here/manifests/" --ignore-not-found
	;;

*)
	echo "usage: $0 [apply|urls|delete]" >&2
	exit 2
	;;
esac
