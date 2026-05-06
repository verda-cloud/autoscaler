/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package verdacloud

import (
	"fmt"
	"strings"
)

// startupEnv is the *complete* set of env vars exposed to the operator's
// startup script. Adding a field is a deliberate Go-code change reviewed in
// PR. Operators cannot extend this set — the script body must work with
// what is exposed here.
//
// Per-VM ProviderID and Labels exist while there is no verdacloud-CCM in
// the cluster. Once a CCM is deployed and the operator's script is updated
// to use --cloud-provider=external, those two fields are unused (the CCM
// patches Node objects post-join from Verda API metadata).
type startupEnv struct {
	// Cluster-wide; sourced from the cluster-autoscaler-startup-env Secret
	// via envFrom on the autoscaler container.
	MasterIP     string
	MasterPort   string
	JoinToken    string
	JoinHashFull string

	// Per-VM; computed by the autoscaler at scale-up time.
	ProviderID string
	Labels     string
}

// renderStartupScript produces the bytes to send to Verda's
// CreateStartupScript API: a generated `export VAR=...` prepend block, then
// the operator's decoded startup script verbatim.
//
// No regex, no template, no validation. Empty values become empty exports;
// the operator is responsible for fail-fast in their bash (`set -u` or
// `${VAR:?required}`).
func renderStartupScript(operatorScript []byte, env startupEnv) []byte {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	fmt.Fprintf(&b, "export MASTER_IP=%s\n", shellQuote(env.MasterIP))
	fmt.Fprintf(&b, "export MASTER_PORT=%s\n", shellQuote(env.MasterPort))
	fmt.Fprintf(&b, "export JOIN_TOKEN=%s\n", shellQuote(env.JoinToken))
	fmt.Fprintf(&b, "export JOIN_HASH_FULL=%s\n", shellQuote(env.JoinHashFull))
	fmt.Fprintf(&b, "export PROVIDER_ID=%s\n", shellQuote(env.ProviderID))
	fmt.Fprintf(&b, "export LABELS=%s\n", shellQuote(env.Labels))
	b.WriteString("\n")
	b.Write(operatorScript)
	return []byte(b.String())
}

// shellQuote returns a safely-quoted single-quoted string suitable for the
// right-hand side of a bash assignment. Handles embedded single quotes via
// the canonical bash trick (`'\''`).
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
