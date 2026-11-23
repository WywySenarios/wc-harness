# Contributing to wc-harness

## Testing

No automated tests: verification is a build check plus two smoke runs. Both runs call the provider and cost money, so do not loop them.

### Prerequisites

- `docker`
- `kubectl`
- LLM API key & endpoint inside `.env`
- clone `wc-tools` and sibling repos in sibling directories

### Local Builds

```sh
gofmt -l .
go vet ./...
go build ./...
```

### Harness Docker Image

```sh
docker build --build-context wctools=../wc-tools -t wc-harness:dev .
```

### Docker Image Smoke

```sh
mkdir -p /tmp/wc-smoke
printf 'Use the bash tool to write "ok" into /workspace/out.txt, then reply DONE.\n' > /tmp/wc-smoke/PROMPT.md
docker run --rm --init --user "$(id -u):$(id -g)" --env-file .env \
  -v /tmp/wc-smoke:/workspace wc-harness:dev
cat /tmp/wc-smoke/out.txt
```

### K3s Cluster

To initialize k3s:

```sh
docker run -d --name k3s --privileged --cgroupns=host \
  -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
  -v k3s-data:/var/lib/rancher/k3s \
  -p 127.0.0.1:6443:6443 \
  rancher/k3s:v1.37.2-k3s1 server \
    --disable=traefik --disable=servicelb --node-name wc-k3s
```

To extract kubeconfig:

```sh
install -d -m 700 ~/.kube/configs
docker exec k3s cat /etc/rancher/k3s/k3s.yaml > ~/.kube/configs/wc-harness
chmod 600 ~/.kube/configs/wc-harness
```

`scripts/kubectl` injects that `KUBECONFIG` to use.

To remove the kubeconfig:

```sh
rm ~/.kube/configs/wc-harness
```

To apply k8s manifests:

```sh
./scripts/kubectl wait --for=condition=Ready node/wc-k3s --timeout=180s
docker save wc-harness:dev | docker exec -i k3s ctr -n k8s.io images import -
./scripts/kubectl -n wc create secret generic wc-llm --from-env-file=.env --dry-run=client -o yaml | ./scripts/kubectl apply -f -
```

The Secret is built from `.env`, so rotating a key means editing `.env` and re-running this step.

To cleanup a previous job and start a fresh one:

```sh
./scripts/kubectl -n wc delete job wc-harness --ignore-not-found
./scripts/kubectl apply -k k8s/
./scripts/kubectl -n wc logs -f job/wc-harness
```

To read output results:

```sh
./scripts/kubectl -n wc wait --for=condition=complete job/wc-harness --timeout=300s
docker exec k3s sh -c 'cat /var/lib/rancher/k3s/storage/pvc-*_wc_wc-workspace/job.txt'
```

Teardown:

```sh
docker rm -f k3s
docker volume rm k3s-data
```
