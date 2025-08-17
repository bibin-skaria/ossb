# OSSB — Open Source Slim Builder

[![DOI](https://zenodo.org/badge/DOI/10.5281/zenodo.16883393.svg)](https://doi.org/10.5281/zenodo.16883393)  
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

A **headless, rootless, multi‑architecture** container image builder powered by **Rootless BuildKit** (`buildkitd` + `buildctl`). OSSB provides a simple, Kaniko/Buildx‑like CLI and an optional Kubernetes Job to build & push OCI images with great caching — and leaves **no long‑running daemon** behind.

---

## ✨ Highlights
- **Headless** one‑shot CLI: `./scripts/ossb build ...`
- **Rootless** by default (`moby/buildkit:rootless`)
- **Multi‑arch** out of the box: `linux/amd64, linux/arm64`
- **Ephemeral** builder: start → build → push → stop
- Works with **Docker** or **Podman** runners
- **Registry cache** support for fast repeat builds
- **Kubernetes Job** mode (no in‑cluster daemon)
- Ready‑to‑use **GitHub Actions** workflow

---

## 🚀 Quick Start (Local)

> Requirements: Docker **or** Podman on your machine/runner. For cross‑arch, install QEMU/binfmt once (see Troubleshooting).

```bash
# optional: authenticate to your registry
docker login ghcr.io  # or: podman login ghcr.io

# build & push a multi‑arch image
./scripts/ossb build \
  --context . \
  --file Dockerfile \
  --tag ghcr.io/you/app:1.0 \
  --platform linux/amd64,linux/arm64 \
  --push \
  --registry-cache ghcr.io/you/app:buildcache
```

> Force Podman: `RUNTIME=podman ./scripts/ossb build ...`

---

## 🔧 CLI Usage

```
ossb build \
  --context <path>            # default: .
  --file <Dockerfile>         # default: Dockerfile in context
  --tag <registry/repo:tag>   # required; comma‑separate for multiple
  --platform <p1,p2,...>      # e.g., linux/amd64,linux/arm64
  --push                      # push to registry (default: false)
  --registry-cache <ref>      # e.g., ghcr.io/you/app:buildcache
  --build-arg KEY=VAL ...     # repeatable
```
Environment:
- `RUNTIME` = `docker` (default) or `podman`
- `DOCKER_CONFIG` is propagated into the builder for registry auth

---

## ☸️ Kubernetes (Ephemeral Build Job)

```bash
# 1) (once per cluster) enable QEMU/binfmt for cross‑arch
kubectl apply -f k8s/binfmt-daemonset.yaml

# 2) registry credentials (uses your local ~/.docker/config.json)
kubectl create ns ci || true
kubectl -n ci create secret generic regcred \
  --from-file=.dockerconfigjson=$HOME/.docker/config.json \
  --type=kubernetes.io/dockerconfigjson

# 3) edit envs in k8s/ossb-job.yaml, then run the build job
kubectl -n ci apply -f k8s/ossb-job.yaml
```

---

## 🤖 GitHub Actions

A ready‑to‑use workflow is included at `.github/workflows/build.yml`. It sets up QEMU and runs the wrapper to build & push a multi‑arch image to GHCR.

---

## 🏗️ Architecture

![OSSB Architecture](diagrams/ossb-architecture.svg)

**Key points**
- **Headless & rootless:** ephemeral `buildkitd` (rootless) started only for the build, then stopped.
- **Multi‑arch builds:** `linux/amd64, linux/arm64` via QEMU/binfmt (one‑time setup per machine/cluster).
- **Two ways to run:**
  1) **Local/CI runner** with Docker/Podman → `scripts/ossb build ...`
  2) **Kubernetes Job**: BuildKit + client Pod → buildctl → push → exit
- **Registry cache** support to speed up subsequent builds.

---

## 🧰 Requirements
- Docker **or** Podman
- For cross‑arch emulation: QEMU/binfmt (install once). Easiest on Docker:
  ```bash
  docker run --privileged --rm tonistiigi/binfmt:qemu-v8
  ```

---

## 🛠️ Troubleshooting
- **NXDOMAIN/push auth issues** → ensure `~/.docker/config.json` is mounted (the wrapper does this automatically if present).
- **Cross‑arch fails** → binfmt/QEMU not installed on the runner. Run the `tonistiigi/binfmt` container once (or apply the DaemonSet in Kubernetes).
- **Build args/secrets** → pass with `--build-arg KEY=VAL`. For advanced features (e.g., mounts, secrets), use BuildKit features in your Dockerfile.

---

## 🤝 Contributing
Pull requests are welcome! Please:
1. **Fork** this repo and create a feature branch
2. Keep PRs focused and small; include docs/tests when relevant
3. Use clear commit messages (Conventional Commits appreciated)

Issues: bug reports, feature requests, and quick fixes are all appreciated.

---

## 📜 License

This project is released under the **Apache‑2.0 License** (see [`LICENSE`](LICENSE)).

---

## 📖 Citation

If you use OSSB in your work, please cite the release:

**DOI:** https://doi.org/10.5281/zenodo.16883393

```bibtex
@software{skaria_ossb_2025,
  author  = {Skaria, Bibin},
  title   = {OSSB — Open Source Slim Builder},
  year    = {2025},
  doi     = {10.5281/zenodo.16883393},
  url     = {https://github.com/ossgenesis/ossb}
}
```

---

### Acknowledgements
- Built on the excellent work of **Moby BuildKit** (rootless) and the container community.