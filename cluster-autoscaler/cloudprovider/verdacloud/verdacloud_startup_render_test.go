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
	body := []byte(`#!/bin/bash
kubeadm join "{{.MasterIP}}:{{.MasterPort}}" \
  --token "{{.JoinToken}}" \
  --discovery-token-ca-cert-hash "{{.JoinHashFull}}"
`)
	got, err := renderStartupScript(body, StartupScriptTemplateData{
		MasterIP:     "10.0.0.10",
		MasterPort:   "6443",
		JoinToken:    "abcdef.0123456789abcdef",
		JoinHashFull: "sha256:1234",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		`kubeadm join "10.0.0.10:6443"`,
		`--token "abcdef.0123456789abcdef"`,
		`--discovery-token-ca-cert-hash "sha256:1234"`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("rendered output missing %q\n--- got ---\n%s", want, got)
		}
	}
}

func TestRenderStartupScript_MissingKeyFailsFast(t *testing.T) {
	// Operator typo: {{.Mastr}} not in StartupScriptTemplateData.
	body := []byte(`kubeadm join "{{.Mastr}}:6443"`)
	_, err := renderStartupScript(body, StartupScriptTemplateData{
		MasterIP: "10.0.0.10",
	})
	if err == nil {
		t.Fatal("expected missingkey error, got nil")
	}
	if !strings.Contains(err.Error(), "Mastr") {
		t.Errorf("error should name the bad key; got: %v", err)
	}
}

func TestRenderStartupScript_ParseErrorFailsFast(t *testing.T) {
	// Unbalanced template syntax.
	body := []byte(`kubeadm join "{{.MasterIP"`)
	_, err := renderStartupScript(body, StartupScriptTemplateData{})
	if err == nil {
		t.Fatal("expected parse error for unbalanced template, got nil")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error should mention parse; got: %v", err)
	}
}

func TestRenderStartupScript_NoTemplateInBody(t *testing.T) {
	// Operator's script with no {{...}} references — should pass through unchanged.
	body := []byte("#!/bin/bash\nset -euo pipefail\necho hello\n")
	got, err := renderStartupScript(body, StartupScriptTemplateData{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("expected pass-through; got:\n%s", got)
	}
}

func TestRenderStartupScript_ConditionalAndLoop(t *testing.T) {
	// Operator using template features beyond plain substitution.
	body := []byte(`{{ if .JoinToken }}--token={{.JoinToken}}{{ end }}`)
	got, err := renderStartupScript(body, StartupScriptTemplateData{
		JoinToken: "abc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != "--token=abc" {
		t.Errorf("conditional rendering failed; got %q", got)
	}

	got, err = renderStartupScript(body, StartupScriptTemplateData{}) // empty token
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != "" {
		t.Errorf("expected empty output for empty token branch; got %q", got)
	}
}
