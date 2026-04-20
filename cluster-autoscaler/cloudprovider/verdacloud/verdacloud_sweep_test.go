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
	"context"
	"fmt"
	"strings"
	"testing"

	apiv1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// makeNode builds a Node fixture with a providerID for sweep tests.
func makeNode(name, providerID string) *apiv1.Node {
	return &apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       apiv1.NodeSpec{ProviderID: providerID},
	}
}

// providerIDFor builds a verdacloud providerID for the given hostname.
func providerIDFor(hostname string) string {
	return fmt.Sprintf("%s%s/%s", testProviderPrefix, testLocation, hostname)
}

func TestSweepOrphanNodes(t *testing.T) {
	// Hostnames created by our registered ASG
	aliveHost := fmt.Sprintf("%s-vm-%s-alive01", testHostnamePrefix, strings.ToLower(testLocation))
	orphanHost := fmt.Sprintf("%s-vm-%s-orphan02", testHostnamePrefix, strings.ToLower(testLocation))
	// A hostname whose prefix does NOT match any ASG — e.g. a control-plane VM
	foreignHost := fmt.Sprintf("controlplane-vm-%s-cp0001", strings.ToLower(testLocation))

	aliveNode := makeNode("alive-node", providerIDFor(aliveHost))
	orphanNode := makeNode("orphan-node", providerIDFor(orphanHost))
	foreignNode := makeNode("cp-node", providerIDFor(foreignHost))
	noProvIDNode := makeNode("legacy-node", "")
	nonVerdaNode := makeNode("aws-node", "aws:///us-east-1a/i-abc")

	kubeClient := fake.NewSimpleClientset(
		aliveNode.DeepCopyObject(),
		orphanNode.DeepCopyObject(),
		foreignNode.DeepCopyObject(),
		noProvIDNode.DeepCopyObject(),
		nonVerdaNode.DeepCopyObject(),
	)

	_, _, asgs := newTestEnv(t)
	asgs.kubeClient = kubeClient

	apiHostnames := map[string]bool{aliveHost: true}

	asgs.sweepOrphanNodes(context.Background(), apiHostnames)

	remaining, err := kubeClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}

	got := make(map[string]bool, len(remaining.Items))
	for _, n := range remaining.Items {
		got[n.Name] = true
	}

	// Orphan should be gone; everything else should survive.
	if got["orphan-node"] {
		t.Errorf("expected orphan-node to be deleted, still present")
	}
	for _, name := range []string{"alive-node", "cp-node", "legacy-node", "aws-node"} {
		if !got[name] {
			t.Errorf("expected %s to remain, was deleted", name)
		}
	}
}

func TestSweepOrphanNodes_NilKubeClient(t *testing.T) {
	// Without a kubeClient (unit-test path), sweep must be a no-op and not panic.
	_, _, asgs := newTestEnv(t)
	asgs.kubeClient = nil
	asgs.sweepOrphanNodes(context.Background(), map[string]bool{})
}

func TestSweepOrphanNodes_ToleratesNotFound(t *testing.T) {
	// Simulate the race where the Node disappears between List and Delete.
	orphanHost := fmt.Sprintf("%s-vm-%s-orphan02", testHostnamePrefix, strings.ToLower(testLocation))
	orphanNode := makeNode("orphan-node", providerIDFor(orphanHost))

	kubeClient := fake.NewSimpleClientset(orphanNode.DeepCopyObject())
	kubeClient.PrependReactor("delete", "nodes", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(apiv1.Resource("nodes"), "orphan-node")
	})

	_, _, asgs := newTestEnv(t)
	asgs.kubeClient = kubeClient

	// No panic, no error path escapes — best-effort contract.
	asgs.sweepOrphanNodes(context.Background(), map[string]bool{})
}

func TestSweepOrphanNodes_DeleteErrorDoesNotAbortLoop(t *testing.T) {
	// First orphan's delete fails, second orphan's delete must still happen.
	orphan1Host := fmt.Sprintf("%s-vm-%s-orphan01", testHostnamePrefix, strings.ToLower(testLocation))
	orphan2Host := fmt.Sprintf("%s-vm-%s-orphan02", testHostnamePrefix, strings.ToLower(testLocation))
	orphan1 := makeNode("orphan-1", providerIDFor(orphan1Host))
	orphan2 := makeNode("orphan-2", providerIDFor(orphan2Host))

	kubeClient := fake.NewSimpleClientset(
		orphan1.DeepCopyObject(),
		orphan2.DeepCopyObject(),
	)
	kubeClient.PrependReactor("delete", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
		da, ok := action.(k8stesting.DeleteAction)
		if !ok {
			return false, nil, nil
		}
		if da.GetName() == "orphan-1" {
			return true, nil, fmt.Errorf("simulated apiserver error")
		}
		return false, nil, nil // fall through to default reactor
	})

	_, _, asgs := newTestEnv(t)
	asgs.kubeClient = kubeClient

	asgs.sweepOrphanNodes(context.Background(), map[string]bool{})

	remaining, _ := kubeClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	names := map[string]bool{}
	for _, n := range remaining.Items {
		names[n.Name] = true
	}
	if !names["orphan-1"] {
		t.Errorf("orphan-1 should still exist after simulated delete failure")
	}
	if names["orphan-2"] {
		t.Errorf("orphan-2 should have been deleted even though orphan-1 delete failed")
	}
}

func TestBelongsToManagedAsg(t *testing.T) {
	_, _, asgs := newTestEnv(t)

	// categorizeInstancesForAsg uses EqualFold to compare the hostname prefix
	// to asg.hostnamePrefix, so the sweep must match that behaviour.
	var b strings.Builder
	for i, r := range testHostnamePrefix {
		if i%2 == 0 {
			b.WriteString(strings.ToUpper(string(r)))
		} else {
			b.WriteString(string(r))
		}
	}
	mixedCasePrefix := b.String()

	cases := []struct {
		name     string
		hostname string
		want     bool
	}{
		{"known prefix matches", fmt.Sprintf("%s-vm-fin-01-abc", testHostnamePrefix), true},
		{"prefix case differs from config", fmt.Sprintf("%s-vm-fin-01-abc", mixedCasePrefix), true},
		{"foreign prefix rejected", "controlplane-vm-fin-01-abc", false},
		{"malformed hostname rejected", "not-a-verda-hostname", false},
		{"empty hostname rejected", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := asgs.belongsToManagedAsg(tc.hostname)
			if got != tc.want {
				t.Errorf("belongsToManagedAsg(%q) = %v, want %v", tc.hostname, got, tc.want)
			}
		})
	}
}
