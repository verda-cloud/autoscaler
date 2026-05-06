# Cluster Autoscaler for VerdaCloud

The cluster autoscaler for VerdaCloud (formerly DataCrunch.io) scales worker nodes.

## Configuration

`VERDA_CLIENT_ID` Required VerdaCloud OAuth2 client ID.

`VERDA_CLIENT_SECRET` Required VerdaCloud OAuth2 client secret.

`VERDA_BASE_URL` Optional VerdaCloud API base URL. Defaults to `https://api.verda.com/v1`.

`VERDA_DEBUG` Optional. Set to `true` or `1` to enable SDK-level detailed logging (request/response traces from the verdacloud SDK). Independent of the `debug` field in the JSON config — either one enables verbose SDK logging.

`VERDA_CLUSTER_CONFIG` Base64 encoded JSON according to the following structure:

```json
{
  "image": {
    "gpu": "24.04.kubernetes1.31.1.cuda12.9.qcow2",
    "cpu": "24.04.kubernetes1.31.1.cuda12.9.qcow2"
  },
  "sshKeyIDs": ["your-ssh-key-id"],
  "billingConfig": {
    "price": "FIXED_PRICE",
    "contract": "PAY_AS_YOU_GO"
  },
  "debug": false,
  "availableLocations": ["FIN-02", "FIN-03"],
  "osVolumeSize": 50,
  "labels": ["env=production"],
  "startupScript": "base64 encoded cloud init script. refer to example config",
  "taints": [],
  "groups": {
    "asg-name-here": {
      "labels": ["hardware=gpu", "team=ai"],
      "taints": [
        { "key": "nvidia.com/gpu", "value": "present", "effect": "NoSchedule" }
      ],
      "billingConfig": {
        "price": "FIXED_PRICE",
        "contract": "SPOT"
      },
      "osVolumeSize": 200,
      "availableLocations": ["FIN-02"]
    }
  }
}

```
## Configuration reference

The JSON above is the authoritative format. This table summarizes top‑level keys for quick reference:

| Key | Type | Required | Default | Notes |
|-----|------|----------|---------|-------|
| image.gpu | string | required | — | Image for GPU nodes provided by VerdaCloud (current: k8s 1.31.1, cuda 12.9). Contact us for other versions. |
| image.cpu | string | required | — | Image for CPU nodes provided by VerdaCloud (current: k8s 1.31.1, cuda 12.9). Contact us for other versions. |
| sshKeyIDs | array<string> | required | — | SSH key IDs to inject. See [Fetching SSH Keys](#fetching-ssh-keys) for details. |
| billingConfig.price | string | optional | `FIXED_PRICE` | Pricing mode. Currently only `FIXED_PRICE` is supported by the API; left empty, the provider sets it to that default. |
| billingConfig.contract | string | optional | `PAY_AS_YOU_GO` | One of `LONG_TERM`, `PAY_AS_YOU_GO`, or `SPOT`. Empty defaults to `PAY_AS_YOU_GO`. |
| debug | bool | optional | false | Enables verbose logging from the verdacloud SDK (request/response traces). Equivalent to setting `VERDA_DEBUG=true` as an environment variable. |
| availableLocations | array<string> | required | — | Location codes eligible for provisioning. Group config overwrites global. |
| osVolumeSize | int | optional | 50 | Size of the OS volume in GB. Group config overwrites global. |
| labels | array<string> | optional | — | Labels to apply to all nodes. Group config merges with global. |
| startupScript | string (base64) | required | — | Base64‑encoded Go text/template. The autoscaler renders it per scale-up, substituting `{{.MasterIP}}`, `{{.MasterPort}}`, `{{.JoinToken}}`, `{{.JoinHashFull}}` from the `cluster-autoscaler-startup-env` Secret. Per-VM identity (provider-id, labels) is set post-join by verdacloud-cloud-controller-manager — not exposed as template variables. See [Startup script template](#startup-script-template) and [`examples/config.json`](examples/config.json). |
| taints | array<object> | optional | — | Standard k8s taint objects applied to nodes |
| groups | map<string,object> | optional | — | Node group definitions overriding defaults. See below for supported keys. |
| reapOrphanNodes | bool | optional | false | Opt-in: when true, the autoscaler deletes K8s `Node` objects whose VerdaCloud VMs have disappeared from the API. Intended only for clusters that **do not** run a verdacloud cloud-controller-manager. Once a CCM is deployed it owns Node lifecycle and this flag should remain off. See [Orphan-node reaping](#orphan-node-reaping) below. |
| reapOrphanNodesAfterCycles | int | optional | 3 | Number of consecutive `Refresh` cycles a hostname must be absent from the VerdaCloud API before its `Node` is deleted. Guards against deleting healthy Nodes on a single bad API response. Only consulted when `reapOrphanNodes` is true. |

### Group Configuration Overrides
The `groups` map allows defining overrides for specific Auto Scaling Groups (ASGs). The format is `"asg-name": { ... }`.
Supported override keys within a group object:
- `labels`: Merges with global labels. Overwrites value if conflict with global label.
- `taints`: Merges with global taints. Overwrites value if conflict with global.
- `availableLocations`: Overwrites global available locations.
- `billingConfig`: Overwrites global billing config.
- `osVolumeSize`: Overwrites global OS volume size. Below 50GB falls back to the global value (or default 50).
- `additionalVolumes`: Per-ASG list of extra volumes (`name`, `size`, `type`) attached on instance creation. Group-only; there is no global equivalent.


`VERDA_CLUSTER_CONFIG_FILE` Can be used as alternative to `VERDA_CLUSTER_CONFIG`. This is the path to a file containing the JSON structure described above. The file will be read and the contents will be used as the configuration.

**NOTE**: In contrast to `VERDA_CLUSTER_CONFIG`, this file is not base64 encoded.

## Helper Commands

### Fetching SSH Keys

You can fetch your SSH keys using the following command:

```bash
curl https://api.verda.com/v1/sshkeys \
  --header 'Authorization: Bearer YOUR_SECRET_TOKEN'
```

### Fetching Images

To find available images for the `image.gpu` and `image.cpu` configuration fields:

```bash
curl https://api.verda.com/v1/images \
  --header 'Authorization: Bearer YOUR_SECRET_TOKEN'
```

Look for the `image_type` value in the response to use in your configuration.

### Fetching Instance Types

To find the correct `instance-type` for the `--nodes` flag, you can fetch the available instance types:

```bash
curl https://api.verda.com/v1/instance-types \
  --header 'Authorization: Bearer YOUR_SECRET_TOKEN'
```

Look for the `instance_type` value in the response to use in your configuration.

Node groups must be defined with the `--nodes=<min-servers>:<max-servers>:<instance-type>:<asg-name>[:<hostname-prefix>]` flag. See [Fetching Instance Types](#fetching-instance-types) to find valid `instance-type` values.

The `hostname-prefix` parameter is optional. If provided, it will be used as the base name for generated hostnames instead of the ASG name.

Multiple flags will create multiple node pools. For example:
```
--nodes=1:5:1A6000.10V:as-test-a6000
--nodes=0:10:CPU.4V.16G:cpu-workers
--nodes=1:3:1H100.20V:gpu-h100-pool
--nodes=1:5:1A100.22V:as-test-1a10022v:custom-node
```

The last example uses a custom hostname prefix `custom-node`, so instances will be named like `custom-node-vm-fin-03-1a2b3c4d` instead of `as-test-1a10022v-vm-fin-03-1a2b3c4d`. The `1a2b3c4d` suffix is an 8-character lowercase hex value derived from a random `uint32`.

You can find a complete deployment sample under [examples/cluster-autoscaler-deployment-example.yaml](examples/cluster-autoscaler-deployment-example.yaml). This single file contains all required Kubernetes resources including namespace, RBAC, secrets, configmap, and deployment. Please be aware that you should change the values within this deployment to reflect your cluster:

- Replace `your-client-id` and `your-client-secret` in the `verdacloud-credentials` Secret
- Set `MASTER_IP`, `MASTER_PORT`, `JOIN_TOKEN`, `JOIN_HASH_FULL` in the `cluster-autoscaler-startup-env` Secret (see [Startup script template](#startup-script-template) below)
- Update `your-ssh-key-id` in the ConfigMap
- Modify the `--nodes` flags to match your desired instance types and scaling limits
- Update the `startupScript` with your actual base64-encoded cluster join template

### Startup script template

The `startupScript` field in `cluster-config.json` is a **Go text/template**.
The autoscaler renders it per scale-up by substituting four cluster-wide
variables sourced from the `cluster-autoscaler-startup-env` Secret:

| Template variable | Source |
|---|---|
| `{{.MasterIP}}` | `MASTER_IP` key in `cluster-autoscaler-startup-env` Secret |
| `{{.MasterPort}}` | `MASTER_PORT` key |
| `{{.JoinToken}}` | `JOIN_TOKEN` key |
| `{{.JoinHashFull}}` | `JOIN_HASH_FULL` key |

The Secret keys are exposed as env vars on the autoscaler container via
`envFrom: secretRef: cluster-autoscaler-startup-env` on the Deployment;
the autoscaler reads them at boot and stores them on `cloudConfig` for
template rendering. This pattern matches the cluster-autoscaler community
convention used by `equinixmetal` and `cherryservers` providers
(`renderTemplate` in their `manager_rest.go`).

```text
K8s Secret (4 keys)         autoscaler container          rendered to Verda API
┌─────────────────────┐     ┌──────────────────┐         ┌──────────────────┐
│ stringData:         │ envFrom │ env vars on  │ render  │ kubeadm join     │
│   MASTER_IP:    ".."│ ───►│ Go process:      │ ───►    │   "10.0.0.10:    │
│   MASTER_PORT:  ".."│     │   MASTER_IP=...  │         │     6443"        │
│   JOIN_TOKEN:   ".."│     │   JOIN_TOKEN=... │         │   --token "abc"  │
│   JOIN_HASH_FULL:".." │   │   ...            │         │   ...            │
└─────────────────────┘     └──────────────────┘         └──────────────────┘
        ▲                                                       ▲
   operator manages                              autoscaler calls
   (sealed-secrets / ESO / Vault)                CreateStartupScript with this
```

The rendered output is sent to Verda's `CreateStartupScript` API; the
returned script ID is referenced in the subsequent `CreateInstance` call;
the per-VM script is deleted after the instance launches (the standard
`createStartupScript` lifecycle in `verdacloud_auto_scaling_groups.go`).

#### Failure modes

- **Operator types `{{.Mastr}}`** (typo) — template fails at scale-up
  with a clear error before any VM is created. `Option("missingkey=error")`
  is set on the template, so referencing a name not in
  `StartupScriptTemplateData` is loud, not silent.
- **Operator's template has bad syntax** (e.g. unbalanced `{{`) — same as
  above, parse error fails fast.
- **Secret value is empty** (e.g. operator forgot to set `JOIN_TOKEN`) —
  template renders with empty string; kubeadm-join will fail at VM boot.
  Use a sealed-secrets / ESO pattern that flags missing values to your
  GitOps system if you want pre-deploy validation.

#### Rotation

Each Secret key is independent. Rotate one without re-encoding any blob:

```bash
NEW_TOKEN=$(kubeadm token create --ttl 24h)
kubectl patch secret cluster-autoscaler-startup-env -n cluster-autoscaler \
  --patch "{\"stringData\":{\"JOIN_TOKEN\":\"${NEW_TOKEN}\"}}"
kubectl rollout restart deployment/cluster-autoscaler -n cluster-autoscaler
```

The new pod re-reads the env var at boot; new VMs use the rotated token.
Existing VMs are unaffected.

In production, source the four values from a secret manager (sealed-secrets,
External Secrets Operator, Vault, etc.). `envFrom: secretRef:` consumes any
Secret resource regardless of how it's populated.

### Per-VM identity (provider-id and labels)

Per-VM identity (`spec.providerID`, cloud labels) is set **after** the
node joins the cluster, by `verdacloud-cloud-controller-manager`. The
rendered startup script sets `--cloud-provider=external` on kubelet and
does NOT need to know its providerID in advance.

This is a hard requirement: the autoscaler does not inject `PROVIDER_ID`
or `LABELS` into the script. Without the CCM, new nodes will register
with the standard `node.cloudprovider.kubernetes.io/uninitialized:NoSchedule`
taint and nothing will remove it. Deploy verdacloud-CCM before deploying
the autoscaler — see the `verdacloud-cloud-controller-manager` repo for
install instructions.

## Development

Make sure you're inside the `cluster-autoscaler` root folder.

1.) Build the docker image:

```bash
make make-image BUILD_TAGS=verdacloud TAG='dev' REGISTRY='verdacloud'
```

2.) Push the docker image:

```bash
make push-image BUILD_TAGS=verdacloud TAG='dev' REGISTRY='verdacloud'
```

**Note:** The `make-image` command automatically builds the code inside Docker, so no separate build step is needed.

## Orphan-node reaping

`reapOrphanNodes` is an opt-in transitional feature for clusters that do not
yet run a verdacloud cloud-controller-manager (CCM). Without a CCM, when a
VerdaCloud VM is deleted (manually, by the autoscaler, or by VerdaCloud)
nothing removes the corresponding Kubernetes `Node` object — orphan Nodes
linger as `NotReady` forever.

When `reapOrphanNodes` is `true`, on each `Refresh` the autoscaler:

1. Lists Nodes whose `providerID` has the `verdacloud://` prefix.
2. Filters to hostnames whose prefix matches one of the registered ASGs
   (so manually-provisioned VerdaCloud VMs such as control-plane nodes
   are never touched).
3. For each remaining Node whose hostname is absent from the latest VerdaCloud
   API response, increments a per-hostname missing-cycle counter.
4. Deletes the Node only after the counter reaches
   `reapOrphanNodesAfterCycles` (default `3`). The counter resets when the
   hostname reappears in a later API response.

This avoids deleting healthy Nodes on a single bad API response (e.g. a
truncated paginated list). With the default cycle count and a 1-minute
refresh interval, deletion happens roughly 3 minutes after a VM goes missing.

When this flag is enabled, the cluster-autoscaler ServiceAccount must have
`delete` permission on `nodes`. The example deployment manifest grants this
permission unconditionally; if you keep `reapOrphanNodes: false`, you may
remove the `delete` verb from the `nodes` ClusterRole rule for least privilege.

**Recommendation**: deploy a verdacloud CCM
(`verdacloud-cloud-controller-manager`) and leave `reapOrphanNodes` at its
default `false`. The CCM owns Node lifecycle by design, follows Kubernetes
conventions, and doesn't require the autoscaler to hold `nodes/delete`.

## Support and caveats

- Hostname format: Instances created by this provider include an internal magic separator (`-vm-`) in their hostname that encodes the ASG name or custom hostname prefix (format: `{hostname-prefix|asg-name}-vm-{location-lowercase}-{8-char-hex}`). The 8-character hex suffix is `fmt.Sprintf("%08x", rand.Uint32())`. The autoscaler relies on this magic separator to identify group membership. Examples: `custom-node-vm-fin-03-1a2b3c4d` or `as-test-1a100-vm-fin-03-deadbeef`.
- No legacy fallback: If instances are created outside this provider with different hostname conventions, they may not be associated with the expected ASG by the autoscaler.
- ProviderID format: `verdacloud://<location>/<hostname>`.

## Debugging

Two independent log channels:

- **Cluster-autoscaler verbosity (`--v=N`)** — controls the standard `klog` output from this provider. The provider logs at `--v=4` (informational, e.g. ASG registration, scale-up/down decisions, sweep cycle counts) and `--v=5` (per-instance availability checks). Higher levels (`--v=6+`) emit more detail from the cluster-autoscaler core but no additional provider-specific output.
- **VerdaCloud SDK detailed logging** — enabled by setting either the `debug` field in the JSON config to `true`, or the `VERDA_DEBUG` env var to `true`/`1`. This causes the SDK to log full HTTP request/response traces, useful for API-level troubleshooting. Disabled by default.