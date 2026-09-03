#!/usr/bin/env bash
set -euo pipefail

PROFILE="${1:-aiprofile}"
MEMORY="${MINIKUBE_MEMORY:-4096}"
CPUS="${MINIKUBE_CPUS:-4}"
KUBELET_SYNC="${KUBELET_SYNC_FREQUENCY:-5s}"

echo "==> Deleting existing profile '${PROFILE}' (if any)"
minikube delete --profile "${PROFILE}" 2>/dev/null || true

echo "==> Starting minikube profile '${PROFILE}' (${CPUS} CPUs, ${MEMORY}MB RAM)"
minikube start \
  --profile "${PROFILE}" \
  --memory="${MEMORY}" \
  --cpus="${CPUS}" \
  --extra-config=kubelet.sync-frequency="${KUBELET_SYNC}"

minikube profile "${PROFILE}"

echo "==> Enabling ingress with SSL passthrough"
minikube addons enable ingress --profile "${PROFILE}"
kubectl wait --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=120s

kubectl patch deployment ingress-nginx-controller -n ingress-nginx \
  --type='json' \
  -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--enable-ssl-passthrough"}]'
kubectl rollout status deployment/ingress-nginx-controller -n ingress-nginx --timeout=120s

echo "==> Installing cert-manager"
helm repo add jetstack https://charts.jetstack.io 2>/dev/null || true
helm repo update jetstack
helm upgrade -i cert-manager jetstack/cert-manager \
  -n cert-manager --create-namespace \
  --set crds.enabled=true \
  --wait

echo "==> Installing trust-manager"
helm upgrade -i trust-manager jetstack/trust-manager \
  -n cert-manager \
  --set secretTargets.enabled=true \
  --set secretTargets.authorizedSecretsAll=true \
  --wait

echo "==> Installing CRDs"
cd "$(dirname "$0")/.."
make install

echo "==> Verifying"
kubectl get nodes
kubectl get pods -n cert-manager
kubectl get crds | grep -E 'broker|activemq'
echo ""
echo "==> Profile '${PROFILE}' ready for E2E tests"
