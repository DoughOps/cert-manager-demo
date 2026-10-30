# DoughOps cert-manager v1.20.3

This fork is based on upstream tag `v1.20.3`
(`1e1d16d1744e6d9c80e464e58e8d9ab1caed222b`). It selectively reverses the
defaulting and gate graduation in upstream commit `71014c3e25673ddc2897edd52fedf64fa5b22e4f`,
restoring the v1.19 `DefaultPrivateKeyRotationPolicyAlways` beta gate. Other
v1.20.3 fixes remain in place; the old admission warning is not restored.

## Configure private key rotation

Set the flag on the **controller**:

```text
--feature-gates=DefaultPrivateKeyRotationPolicyAlways=false
```

Or use the v1.20.3 Helm chart with these values:

```yaml
featureGates: DefaultPrivateKeyRotationPolicyAlways=false
```

| Certificate privateKey.rotationPolicy | Gate true (default) | Gate false |
| --- | --- | --- |
| omitted, including privateKey omitted | Always | Never |
| Always | Always | Always |
| Never | Never | Never |

The gate affects runtime defaults in the controller, not the persisted
Certificate spec. Restart all controller replicas with the same gate value.
It does not undo keys already rotated. `Never` reuses a compatible existing
private key; an absent key still needs to be generated. Use explicit
`rotationPolicy: Always` when renewal must replace a compromised key.
As in v1.19, `AllBeta=false` also disables this restored beta gate unless it is
explicitly enabled.

## Build and publish images

`.github/workflows/doughops-images.yml` runs on PRs, pushes to master, main,
release-1.20 and doughops-v1.20.3-rotation, tags matching `v1.20.3-doughops.*`,
and manual dispatch. Tests must pass before image builds start. PRs build and
run `--help` without registry login or push. Push/manual runs authenticate with
the built-in `GITHUB_TOKEN` (`packages: write`); no PAT is needed.

The workflow builds **linux/amd64** images using the upstream Make targets,
digest-pinned base images, version metadata and license notices:

```text
ghcr.io/doughops/cert-manager-demo/controller:v1.20.3-doughops-<12-character-sha>
ghcr.io/doughops/cert-manager-demo/webhook:v1.20.3-doughops-<12-character-sha>
ghcr.io/doughops/cert-manager-demo/cainjector:v1.20.3-doughops-<12-character-sha>
ghcr.io/doughops/cert-manager-demo/acmesolver:v1.20.3-doughops-<12-character-sha>
ghcr.io/doughops/cert-manager-demo/startupapicheck:v1.20.3-doughops-<12-character-sha>
```

Owner/repository names are lowercased, following image-builder. A release tag
also produces that exact tag on each component. There is no shared `latest`
tag or package deletion. First-time GHCR packages may be private; configure
package visibility or Kubernetes imagePullSecrets as appropriate. GitHub must
enable Actions and permit package writes for this repository.

Set all five Helm image repositories to the corresponding paths above and use
the same built tag. The Helm value paths are `image`, `webhook.image`,
`cainjector.image`, `acmesolver.image` and `startupapicheck.image`, each with
`repository` and `tag`. Remove any old `digest` override. In particular, the
controller must use the patched image for the gate to take effect. Keep the
v1.20.3 chart/CRDs rather than deploying this image with newer master manifests.

## Reproduce validation

From the checkout root:

```sh
make _bin/tools/go _bin/tools/etcd _bin/tools/kube-apiserver _bin/tools/kubectl
export GOFLAGS=-mod=readonly GOMAXPROCS=3
_bin/tools/go test -p 3 -count=1 ./internal/apis/certmanager/v1 ./internal/apis/certmanager/validation ./pkg/controller/certificates/...
export KUBEBUILDER_ASSETS="$PWD/_bin/tools"
(cd test/integration && ../../_bin/tools/go test -p 3 -count=1 -timeout=10m -v -run 'TestDefaultPrivateKeyRotation|TestGeneratesNewPrivateKey' ./certificates)
make _bin/containers/cert-manager-controller-linux-amd64.tar VERSION=v1.20.3-doughops-local GOBUILDPROCS=3
docker run --rm cert-manager-controller-amd64:v1.20.3-doughops-local --help
```

The integration experiment starts real etcd and kube-apiserver processes and
runs the Certificate controllers. Eight cases compare each generated CSR's
public key with a pre-existing Secret's key. The two existing re-issuance tests
also verify new private keys across CertificateRequests. This experiment does
not require an external issuer, registry write, or production cluster.

In restricted cloud environments, Go's public proxy can redirect module
archives to `storage.googleapis.com`; that host must be allowed alongside
`proxy.golang.org` and `sum.golang.org`. Keep checksum and TLS verification on.

## Cloud validation results

On 2026-10-30, Go 1.26.4 (the v1.20.3 Makefile pin) passed all nine tested
unit-test packages in the command above. The feature registration, parent
Certificate package and metrics package have no tests and are not counted.
Actionlint v1.7.7 and `git diff --check` also passed.

After the environment network update allowed `storage.googleapis.com`, the
complete `test/integration/certificates` package passed: **14 top-level tests,
zero failures and zero skips**, in 120.84 seconds. This includes all eight gate
matrix cases comparing actual keys and the two existing re-issuance tests. The
suite used real Kubernetes v1.35 test assets and ran without a `-run` filter:

```sh
export KUBEBUILDER_ASSETS="$PWD/_bin/tools"
(cd test/integration && GOFLAGS=-mod=readonly GOMAXPROCS=2 ../../_bin/tools/go test -p 2 -count=1 -timeout=10m -json ./certificates)
```

All five linux/amd64 upstream container targets built successfully: controller,
webhook, cainjector, acmesolver and startupapicheck. Each exported image archive
was loaded and each container's `--help` exited successfully. Architecture and
OCI source labels were verified; all source labels point to
`https://github.com/DoughOps/cert-manager-demo`. The Makefile's archive name
includes `linux-amd64`, while its local image tag includes only `amd64`;
the workflow uses this verified tag when running and retagging images.

The controller image also accepted both
`--feature-gates=DefaultPrivateKeyRotationPolicyAlways=true` and `false` during
normal startup (without `--help`), reaching the expected error for a deliberately
missing kubeconfig. An invalid boolean was rejected by the flag parser. These
CLI checks verify argument handling; the real-key behavior is verified by the
integration matrix above.

The earlier module-download HTTP 403 blocker is resolved. Validation logs and a
machine-readable summary are retained at
`/workspace/onboarding/rotation-final-validation/`. GHCR pushing and an actual
GitHub Actions run have not been performed from this checkout. Kubernetes E2E
and integration packages outside `certificates` were not run.
