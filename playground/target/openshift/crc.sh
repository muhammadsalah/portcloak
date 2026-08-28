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
#   playground/target/openshift/crc.sh setup    # configure and start
#   playground/target/openshift/crc.sh stop
#   playground/target/openshift/crc.sh delete   # and its disk
#
# CRC itself is not installed here. It needs a Red Hat pull secret, which is an
# account-bound file this repository must not carry.

set -euo pipefail

CPUS=${CRC_CPUS:-8}
MEMORY_MIB=${CRC_MEMORY_MIB:-32768}
DISK_GIB=${CRC_DISK_GIB:-100}

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

	# The monitoring stack is the largest thing in a default CRC that nothing
	# here looks at. Off, it is roughly 4 GiB back for the workloads.
	crc config set enable-cluster-monitoring false

	echo "cpus=$CPUS memory=${MEMORY_MIB}MiB disk=${DISK_GIB}GiB"
}

case "${1:-setup}" in
setup)
	configure
	crc setup
	crc start
	cat <<-'NEXT'

		Log in, then apply the playground:

		  eval $(crc oc-env)
		  crc console --credentials        # prints the kubeadmin password
		  oc login -u kubeadmin -p <password> https://api.crc.testing:6443
		  playground/target/openshift/apply.sh
	NEXT
	;;
config)
	configure
	;;
stop)
	crc stop
	;;
delete)
	crc delete --force
	;;
*)
	echo "usage: $0 [setup|config|stop|delete]" >&2
	exit 2
	;;
esac
