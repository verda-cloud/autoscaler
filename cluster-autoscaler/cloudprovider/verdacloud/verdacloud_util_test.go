/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package verdacloud

import (
	"strings"
	"testing"
)

// Test constants for util tests
const (
	testUtilInstanceType   = "CPU.4V.16G"
	testUtilGPUType        = "1H100.80S.22V"
	testUtilAsgName        = "asg-test"
	testUtilProviderPrefix = "verdacloud://"
)

func TestParseAsgSpec_Valid(t *testing.T) {
	spec := "1:10:" + testUtilInstanceType + ":" + testUtilAsgName
	got, err := parseAsgSpec(spec)
	if err != nil {
		t.Fatalf("parseAsgSpec returned error: %v", err)
	}
	if got.minSize != 1 || got.maxSize != 10 || got.instanceType != testUtilInstanceType || got.name != testUtilAsgName {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestParseAsgSpec_InvalidFormat(t *testing.T) {
	_, err := parseAsgSpec("1:10:" + testUtilInstanceType) // only 3 parts
	if err == nil {
		t.Fatalf("expected error for invalid format, got nil")
	}
}

func TestInstanceRefFromProviderId_Valid(t *testing.T) {
	pid := testUtilProviderPrefix + "FIN-03/asg-x-77-FIN-03-1700000000"
	ref, err := instanceRefFromProviderId(pid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.ProviderID != pid {
		t.Fatalf("providerID mismatch: %s", ref.ProviderID)
	}
	if ref.Hostname != "asg-x-77-FIN-03-1700000000" {
		t.Fatalf("hostname mismatch: %s", ref.Hostname)
	}
}

func TestInstanceRefFromProviderId_Invalid(t *testing.T) {
	_, err := instanceRefFromProviderId("verdacloud://malformed")
	if err == nil {
		t.Fatalf("expected error for malformed provider id, got nil")
	}
}

func TestExtractAsgNameFromHostname_NewFormat(t *testing.T) {
	host := "asg-prod-vm-fin-03-77"
	name, err := extractAsgNameFromHostname(host)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "asg-prod" {
		t.Fatalf("expected asg-prod, got %s", name)
	}
}

func TestExtractAsgNameFromHostname_NoMagic(t *testing.T) {
	_, err := extractAsgNameFromHostname("asg-prod-FIN-03-1700000000")
	if err == nil {
		t.Fatalf("expected error when magic separator is missing")
	}
}

func TestIsGPUInstanceType(t *testing.T) {
	if !isGPUInstanceType(testUtilGPUType) {
		t.Fatalf("expected GPU type to be detected")
	}
	if isGPUInstanceType(testUtilInstanceType) {
		t.Fatalf("expected CPU type not to be detected as GPU")
	}
}

// --------------------------------------------------------------------------
// renderStartupScript tests
// --------------------------------------------------------------------------

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
