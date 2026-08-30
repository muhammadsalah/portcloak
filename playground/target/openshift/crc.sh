#!/usr/bin/env bash
# Copyright 2026 Muhammad Salah
# SPDX-License-Identifier: Apache-2.0
#
# The CRC virtual machine the OpenShift playground runs in.
#
# Sizing is not a guess. Three Keycloaks each want a JVM heap and each runs an
# ephemeral clone during a capture — a fourth JVM, briefly, with the same image
# and the same database connection. Add Postgres, two directories, and
# OpenShift's own control plane, which is most of a CRC instance's floor before
# anything of ours is scheduled. Eight cores and 32 GiB is what leaves headroom
# for the clone rather than making the clone the thing that gets evicted.
#
# The disk is 100 GiB because a realm with 160,000 users exports to a directory
# of user files, and the clone writes that inside the pod before PortCloak
# streams it out.
#
# The monitoring stack costs roughly 4 GiB of that headroom, so on a 32 GiB VM
# the clone is closer to being the thing that gets evicted than the paragraph
# above assumes. If a capture dies with a pod OOMKilled or Evicted, that is the
# first thing to suspect: raise CRC_MEMORY_MIB, or set enable-cluster-monitoring
# back to false and lose `top`.
#
#   playground/target/openshift/crc.sh setup    # configure, start, wait, report
#   playground/target/openshift/crc.sh status   # CPU and memory, VM and pods
#   playground/target/openshift/crc.sh stop
#   playground/target/openshift/crc.sh delete   # and its disk
#
# CRC itself is not installed here. It needs a Red Hat pull secret, which is an
# account-bound file this repository must not carry.

set -euo pipefail

CPUS=${CRC_CPUS:-8}
MEMORY_MIB=${CRC_MEMORY_MIB:-32768}
DISK_GIB=${CRC_DISK_GIB:-100}

# Where the playground lives, and so where an ephemeral clone appears during a
# capture. Matches manifests/00-namespace.yaml.
NAMESPACE=${CRC_NAMESPACE:-kc}

need() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "!! $1 is not on PATH. $2" >&2
		exit 1
	}
}

configure() {
	need crc "Install CRC from https://console.redhat.com/openshift/create/local"

	# Set before start, not after: CRC applies cpus and memory when it creates
	# the VM, and changing them on a running instance is a stop and a start.
	crc config set cpus "$CPUS"
	crc config set memory "$MEMORY_MIB"
	crc config set disk-size "$DISK_GIB"

	# On, so that `oc adm top` answers and the console draws graphs. It is the
	# largest thing in a default CRC and nothing PortCloak does needs it — but
	# working out whether a capture is being starved of CPU, or how much a
	# 160,000-user export actually costs the clone, needs numbers rather than a
	# guess, and there is nowhere else to get them.
	#
	# It is what metrics-server rides in on. Without it `oc adm top nodes`
	# reports "Metrics API not available" and the status command below has
	# nothing to print.
	crc config set enable-cluster-monitoring true

	echo "cpus=$CPUS memory=${MEMORY_MIB}MiB disk=${DISK_GIB}GiB"
}

# `crc start` returns when the VM is up and the API is answering. That is not
# the same as a cluster you can work in: the operators are still rolling out
# behind it, and with monitoring enabled metrics-server is among the last things
# to arrive. Running apply.sh into that gap fails in ways that look like a
# problem with the manifests — a webhook that is not listening yet, an
# imagestream that resolves to nothing — so this waits instead.
#
# Everything here is a poll rather than `oc wait`, because `oc wait` needs the
# resource to already exist and half of what we are waiting for is it appearing.
ready() {
	need oc "Install the OpenShift client, or run: eval \$(crc oc-env)"

	local began=$SECONDS
	# CRC writes a kubeconfig with a kubeadmin context on start, so there is
	# nothing to log in to first.
	wait_for "the API server" api_answering || return 1
	wait_for "the cluster operators" operators_available || return 1
	# The one that decides whether `top` works, and the reason monitoring is on.
	# It answers a beat after its pod is Running, so this asks the API the same
	# question the status command will rather than trusting the pod phase.
	wait_for "the metrics API" metrics_answering || return 1

	echo "  ready in $((SECONDS - began))s"
}

api_answering() { oc get --raw='/readyz' >/dev/null 2>&1; }

# An operator that is Available is one whose thing works. Progressing on its own
# is fine — that is a rollout in flight — but Available=False means something
# downstream of it is not there yet, and that is the wait.
#
# The emptiness check matters as much as the grep: before the operators exist at
# all the list is empty, and "no operator reports False" would be trivially true.
operators_available() {
	local states
	states=$(oc get clusteroperators \
		-o jsonpath='{range .items[*]}{.status.conditions[?(@.type=="Available")].status}{"\n"}{end}' 2>/dev/null) || return 1
	[ -n "$states" ] || return 1
	! printf '%s\n' "$states" | grep -qx "False"
}

metrics_answering() { oc adm top nodes >/dev/null 2>&1; }

# wait_for keeps the waiting legible: one line naming what is being waited for, a
# dot per poll, and a deadline that says which wait gave up rather than hanging
# until somebody presses Ctrl-C.
wait_for() {
	local what=$1 check=$2
	local deadline=$((SECONDS + ${CRC_READY_TIMEOUT:-900}))

	printf '  waiting for %s' "$what"
	until "$check"; do
		if [ "$SECONDS" -ge "$deadline" ]; then
			printf ' gave up\n'
			echo "!! $what did not come up within ${CRC_READY_TIMEOUT:-900}s" >&2
			echo "   try: crc status, and oc get clusteroperators" >&2
			return 1
		fi
		printf '.'
		sleep 5
	done
	printf ' ok\n'
}

# What the VM is doing, and what is on it.
#
# Three levels, because they answer different questions: crc status is the VM's
# own accounting, `top nodes` is what the cluster thinks it is using, and `top
# pods` is which of ours is responsible. During a capture the interesting row is
# the ephemeral clone, which appears in the playground namespace for as long as
# the export runs and is gone afterwards.
status() {
	need crc "Install CRC from https://console.redhat.com/openshift/create/local"
	crc status

	need oc "Install the OpenShift client, or run: eval \$(crc oc-env)"
	echo
	if ! oc adm top nodes 2>/dev/null; then
		# Distinguish "nothing to report" from "I could not look". Metrics
		# missing usually means monitoring was turned off, or the cluster has
		# not finished coming up.
		echo "the metrics API is not answering." >&2
		echo "  enable-cluster-monitoring is $(crc config get enable-cluster-monitoring 2>&1 | sed 's/.*: //')" >&2
		echo "  if that says true, the cluster may still be starting: $0 ready" >&2
		return 1
	fi

	echo
	if [ -n "$(oc get pods -n "$NAMESPACE" -o name 2>/dev/null)" ]; then
		echo "namespace $NAMESPACE:"
		oc adm top pods -n "$NAMESPACE" 2>/dev/null || true
	else
		echo "namespace $NAMESPACE holds nothing yet — playground/target/openshift/apply.sh"
	fi
}

case "${1:-setup}" in
setup)
	configure
	crc setup
	crc start
	# oc has to be on PATH for the wait, and crc knows where its own copy is.
	# Doing it here rather than telling the operator to means setup either
	# finishes against a cluster that works or says which wait it gave up on.
	eval "$(crc oc-env)"
	ready
	echo
	status || true
	cat <<-'NEXT'

		The cluster is up and the metrics API is answering. To use it:

		  eval $(crc oc-env)
		  playground/target/openshift/apply.sh

		crc console --credentials prints the kubeadmin password, for the web
		console. The kubeconfig crc start wrote is already good for oc.
	NEXT
	;;
config)
	configure
	;;
ready)
	eval "$(crc oc-env)"
	ready
	;;
status)
	eval "$(crc oc-env)" 2>/dev/null || true
	status
	;;
stop)
	crc stop
	;;
delete)
	crc delete --force
	;;
*)
	echo "usage: $0 [setup|config|ready|status|stop|delete]" >&2
	exit 2
	;;
esac
