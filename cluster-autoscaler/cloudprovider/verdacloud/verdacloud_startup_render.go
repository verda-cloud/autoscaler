/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package verdacloud

import (
	"bytes"
	"fmt"
	"text/template"
)

// StartupScriptTemplateData defines the *complete* set of variables the
// operator's startupScript may reference via Go text/template syntax.
//
// Adding a field here is a deliberate Go-code change reviewed in PR.
// Operators cannot extend this set — the script body must work with
// what is exposed here, or template execution fails fast (we set
// Option("missingkey=error") on the template).
//
// Per-VM identity (PROVIDER_ID, LABELS) is intentionally NOT in this
// struct. Once the verdacloud cloud-controller-manager runs, it patches
// spec.providerID and the standard topology / instance-type labels onto
// each Node post-join. The script does not need to know its providerID
// in advance — kubelet starts with --cloud-provider=external.
//
// Pattern reference: this mirrors equinixmetal/manager_rest.go's
// CloudInitTemplateData (BootstrapTokenID / BootstrapTokenSecret /
// APIServerEndpoint / NodeGroup) and cherryservers' equivalent.
type StartupScriptTemplateData struct {
	// Cluster-wide; sourced from the cluster-autoscaler-startup-env Secret
	// via envFrom on the autoscaler container.
	MasterIP     string
	MasterPort   string
	JoinToken    string
	JoinHashFull string
}

// renderStartupScript executes the operator's startupScript template against
// the cluster-wide values and returns the bytes to send to Verda's
// CreateStartupScript API.
//
// Failure modes (caught at scale-up time, before any VM is created):
//   - parse error: operator's template has invalid Go-template syntax.
//   - missing-key error: operator referenced {{.NotInStruct}} that doesn't
//     exist on StartupScriptTemplateData. Typo detection.
//   - execute error: anything else surfaced by template.Execute.
func renderStartupScript(operatorBody []byte, vars StartupScriptTemplateData) ([]byte, error) {
	tmpl, err := template.New("startupScript").
		Option("missingkey=error").
		Parse(string(operatorBody))
	if err != nil {
		return nil, fmt.Errorf("parse startupScript template: %w", err)
	}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, vars); err != nil {
		return nil, fmt.Errorf("execute startupScript template: %w", err)
	}
	return out.Bytes(), nil
}
