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
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"

	apiv1 "k8s.io/api/core/v1"
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

func TestParseAsgSpec_NameRoundTripsThroughHostname(t *testing.T) {
	for _, name := range []string{"a", "pool1", "gpu-h100", "vm", "vm-pool", "a-vmx", "x-v-m"} {
		t.Run(name, func(t *testing.T) {
			for _, spec := range []string{"0:1:CPU.4V:" + name, "0:1:CPU.4V:base:" + name} {
				got, err := parseAsgSpec(spec)
				if err != nil {
					t.Fatalf("parseAsgSpec(%q): %v", spec, err)
				}
				base := got.hostnamePrefix
				if base == "" {
					base = got.name
				}
				hostname := base + ASG_SEPARATOR + "fin-01-deadbeef"
				parsed, err := extractAsgNameFromHostname(hostname)
				if err != nil || parsed != base {
					t.Errorf("hostname %q parses to (%q, %v), want %q", hostname, parsed, err, base)
				}
			}
		})
	}
}

func TestParseAsgSpec_RejectsNamesThatBreakHostnameParsing(t *testing.T) {
	for _, name := range []string{"gpu.h100", "GPU", "Pool", "gpu_h100", "cpu-vm", "gpu-vm-pool", "-a", "a-", "a--vm"} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseAsgSpec("0:1:CPU.4V:" + name); err == nil {
				t.Errorf("ASG name %q accepted", name)
			}
			if _, err := parseAsgSpec("0:1:CPU.4V:ok:" + name); err == nil {
				t.Errorf("hostname prefix %q accepted", name)
			}
		})
	}
}

func TestLocationFromProviderID(t *testing.T) {
	cases := map[string]string{
		"verdacloud://fin-01/host-vm-fin-01-1": "fin-01",
		"verdacloud:///host":                   "",
		"verdacloud://fin-01":                  "",
		"aws:///us-east-1a/i-1":                "",
		"":                                     "",
	}
	for in, want := range cases {
		if got := locationFromProviderID(in); got != want {
			t.Errorf("locationFromProviderID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKubeletFlags(t *testing.T) {
	if got := kubeletNodeLabelsFlag(map[string]string{"b": "2", "a": "1"}); got != "a=1,b=2" {
		t.Errorf("labels flag = %q", got)
	}
	if got := kubeletNodeLabelsFlag(nil); got != "" {
		t.Errorf("empty labels flag = %q, want empty", got)
	}
	taints := []apiv1.Taint{
		{Key: "z", Effect: apiv1.TaintEffectNoExecute},
		{Key: "dedicated", Value: "gpu", Effect: apiv1.TaintEffectNoSchedule},
	}
	if got := kubeletTaintsFlag(taints); got != "dedicated=gpu:NoSchedule,z:NoExecute" {
		t.Errorf("taints flag = %q", got)
	}
	if got := kubeletTaintsFlag(nil); got != "" {
		t.Errorf("empty taints flag = %q, want empty", got)
	}
}

// The shipped example startup script must hand the per-group labels and taints to kubelet;
// otherwise real nodes miss the labels the template node advertises.
func TestExampleStartupScriptPassesNodeLabelsAndTaintsToKubelet(t *testing.T) {
	raw, err := os.ReadFile("examples/config.json")
	if err != nil {
		t.Fatalf("read example config: %v", err)
	}
	var cfg cloudConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse example config: %v", err)
	}
	script, err := base64.StdEncoding.DecodeString(cfg.StartupScript)
	if err != nil {
		t.Fatalf("decode startupScript: %v", err)
	}

	deployment, err := os.ReadFile("examples/cluster-autoscaler-deployment-example.yaml")
	if err != nil {
		t.Fatalf("read deployment example: %v", err)
	}
	if !strings.Contains(string(deployment), cfg.StartupScript) {
		t.Error("deployment example embeds a different startupScript than examples/config.json")
	}

	base := StartupScriptTemplateData{MasterIP: "10.0.0.1", MasterPort: "6443", JoinToken: "REPLACE_ME_TOKEN", JoinHashFull: "sha256:REPLACE_ME"}

	withTaints := base
	withTaints.NodeLabels = "verda.com/node-group=gpu-pool"
	withTaints.NodeTaints = "nvidia.com/gpu=present:NoSchedule"
	out, err := renderStartupScript(script, withTaints)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{"--node-labels=verda.com/node-group=gpu-pool", "--register-with-taints=nvidia.com/gpu=present:NoSchedule", "--cloud-provider=external"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("rendered script lacks %q", want)
		}
	}

	noTaints := base
	noTaints.NodeLabels = "verda.com/node-group=cpu-pool"
	out, err = renderStartupScript(script, noTaints)
	if err != nil {
		t.Fatalf("render without taints: %v", err)
	}
	if strings.Contains(string(out), "--register-with-taints") {
		t.Error("empty NodeTaints must not emit --register-with-taints")
	}
}
