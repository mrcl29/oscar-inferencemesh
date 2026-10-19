#!/usr/bin/env python3
"""
Utility script to patch OSCAR's kind-deploy.sh for non-interactive execution,
resilient apiserver readiness polling, and internal DNS resolution.
"""

import sys


def patch_kind_deploy(filepath: str) -> None:
    with open(filepath, "r") as f:
        content = f.read()

    # 1. Inject DNS fix and retry loop in createKindCluster
    target1 = """    if ! kubectl cluster-info --context "$KIND_CONTEXT" &> /dev/null; then
        echo -e "$RED[*]$END_COLOR Kind cluster not found."
        echo "Stopping execution ...."
        if [ -f $CONFIG_FILEPATH ]; then 
            rm $CONFIG_FILEPATH
        fi
        exit
    fi"""

    replacement1 = """    echo "[*] Ensuring DNS resolver inside Kind node..."
    docker exec "$CLUSTER_NAME-control-plane" bash -c "echo -e \\\"nameserver 1.1.1.1\\nnameserver 8.8.8.8\\\" > /etc/resolv.conf" 2>/dev/null || true
    echo "[*] Waiting for Kubernetes control plane to be responsive..."
    local ready=false
    for i in $(seq 1 30); do
        if kubectl cluster-info --context "$KIND_CONTEXT" &> /dev/null; then
            ready=true
            break
        fi
        sleep 2
    done
    if [ "$ready" != "true" ]; then
        echo -e "$RED[*]$END_COLOR Kind cluster apiserver not responding after 60 seconds."
        [ -f "$CONFIG_FILEPATH" ] && rm "$CONFIG_FILEPATH"
        exit 1
    fi"""

    if target1 in content:
        content = content.replace(target1, replacement1)

    # 2. Ensure Traefik status timeout fails with exit 1 instead of continuing
    target2 = """            echo -e "\\n$RED[!]$END_COLOR Error: Timeout: Traefik pod status: $traefik_status"
            exit"""

    replacement2 = """            echo -e "\\n$RED[!]$END_COLOR Error: Timeout: Traefik pod status: $traefik_status"
            exit 1"""

    if target2 in content:
        content = content.replace(target2, replacement2)

    with open(filepath, "w") as f:
        f.write(content)


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: patch_kind_deploy.py <path_to_kind_deploy.sh>", file=sys.stderr)
        sys.exit(1)
    patch_kind_deploy(sys.argv[1])
