/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package verdacloud

import (
	"strings"
	"testing"
)

func TestRenderStartupScript_HappyPath(t *testing.T) {
	env := startupEnv{
		MasterIP:     "10.0.0.10",
		MasterPort:   "6443",
		JoinToken:    "abcdef.0123456789abcdef",
		JoinHashFull: "sha256:1234",
		ProviderID:   "verdacloud://FIN-02/host-1a2b3c4d",
		Labels:       "env=production,hardware=gpu",
	}
	body := []byte("kubeadm join \"${MASTER_IP}:${MASTER_PORT}\" --token \"${JOIN_TOKEN}\"\n")

	got := string(renderStartupScript(body, env))

	for _, want := range []string{
		"#!/usr/bin/env bash\n",
		"export MASTER_IP='10.0.0.10'\n",
		"export MASTER_PORT='6443'\n",
		"export JOIN_TOKEN='abcdef.0123456789abcdef'\n",
		"export JOIN_HASH_FULL='sha256:1234'\n",
		"export PROVIDER_ID='verdacloud://FIN-02/host-1a2b3c4d'\n",
		"export LABELS='env=production,hardware=gpu'\n",
		"kubeadm join", // operator body passed through verbatim
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered script missing %q\n--- script ---\n%s", want, got)
		}
	}
}

func TestRenderStartupScript_EmptyValuesAllowed(t *testing.T) {
	// Empty env values produce empty `export FOO=''` — operator must use
	// `set -u` or `${FOO:?required}` to fail fast on missing values.
	env := startupEnv{}
	body := []byte("echo done\n")

	got := string(renderStartupScript(body, env))

	if !strings.Contains(got, "export MASTER_IP=''\n") {
		t.Errorf("empty MasterIP should render as empty export; got:\n%s", got)
	}
	if !strings.HasSuffix(got, "echo done\n") {
		t.Errorf("operator body should be at the end of the rendered script; got:\n%s", got)
	}
}

func TestShellQuote_HandlesSingleQuotes(t *testing.T) {
	cases := map[string]string{
		``:                    `''`,
		`abc`:                 `'abc'`,
		`a b c`:               `'a b c'`,
		`don't`:               `'don'\''t'`,
		`'leading`:            `''\''leading'`,
		`trailing'`:           `'trailing'\'''`,
		`multi'quote'string`:  `'multi'\''quote'\''string'`,
	}
	for input, want := range cases {
		if got := shellQuote(input); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRenderStartupScript_BodyByteEqualityAfterPrepend(t *testing.T) {
	body := []byte("#!/bin/bash\nset -euo pipefail\necho hi\n")
	out := renderStartupScript(body, startupEnv{})

	// The operator body should appear verbatim somewhere in the output.
	if !strings.Contains(string(out), string(body)) {
		t.Errorf("operator body not found verbatim in output; got:\n%s", string(out))
	}
}
