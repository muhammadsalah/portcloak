// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package local_test

import (
	"testing"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/target"
	"portcloak/internal/engine/target/local"
	"portcloak/internal/engine/target/targettest"
)

// The local executor is the one adapter that can run the Executor table without
// a container, an sshd or a cluster, so it is where a divergence shows up on
// every commit rather than only under the integration tag.
func TestLocal_Contract(t *testing.T) {
	targettest.RunContract(t, func(t *testing.T) target.Executor {
		return local.New(config.Environment{Name: "laptop", Kind: config.EnvLocal})
	})
}
