# Gateway API Controller — Example Installation for local machine

This directory contains sample manifests and step-by-step instructions for deploying a Gateway API controller on Kubernetes local setup.

## Prerequisites

- A running Kubernetes cluster (tested with Docker Desktop)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- Your controller image built locally as `gw-controller:local`
The image can be built by running the following command from the root of this repo:
```bash
CGO_ENABLED=0 GOOS=linux go build -o api-gateway-controller cmd/main.go && docker build -t gw-controller:local .
```

## Installation Steps

### 1. Install Gateway API CRDs

Apply the latest CRDs directly from the official repository:

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.3.0/standard-install.yaml
```


### 2. Create the Namespace

```bash
kubectl apply -f namespace.yaml
```


### 3. Create the serverscom API Secret

Update the values to your actual credentials before applying:

```bash
kubectl apply -f secret.yaml
```


### 4. Create ServiceAccount, RBAC, and Bindings

```bash
kubectl apply -f rbac.yaml
```


### 5. Deploy the Gateway Controller

```bash
kubectl apply -f deployment.yaml
```

## Files Overview

- `namespace.yaml` — Namespace for the controller
- `secret.yaml` — API credentials for serverscom (update these for your environment)
- `rbac.yaml` — ServiceAccount and required RBAC permissions
- `deployment.yaml` — Deployment manifest for the controller

> **Note:** Adjust the image ( image with gw controller should exist ), environment variables, and secrets as needed for your cluster.