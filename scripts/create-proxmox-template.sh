#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# create-proxmox-template.sh
# Creates a Proxmox VE Cloud-Init template for use with Genesis.
#
# Requirements on Proxmox VE host:
#   - qm (QEMU/KVM Virtual Machine Manager CLI)
#   - curl or wget
#
# Usage:
#   bash create-proxmox-template.sh [OPTIONS]
#
# Options:
#   --vmid <ID>         VM ID to use for the template (default: 3000)
#   --name <NAME>       Name of the template (default: genesis-template)
#   --storage <STORAGE> Storage pool to place the disk on (default: SSD)
#   --bridge <BRIDGE>   Network bridge interface (default: vmbr0)
#   --os <OS>           Target OS: debian-12 or ubuntu-24.04 (default: ubuntu-24.04)
#   --memory <MB>       Template RAM in MB (default: 2048)
#   --cores <N>         Template CPU cores (default: 2)
#   -h, --help          Show this help message

set -euo pipefail

VMID="3000"
NAME="genesis-template"
STORAGE="SSD"
BRIDGE="vmbr0"
OS="ubuntu-24.04"
MEMORY="2048"
CORES="2"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --vmid)
      VMID="$2"
      shift 2
      ;;
    --name)
      NAME="$2"
      shift 2
      ;;
    --storage)
      STORAGE="$2"
      shift 2
      ;;
    --bridge)
      BRIDGE="$2"
      shift 2
      ;;
    --os)
      OS="$2"
      shift 2
      ;;
    --memory)
      MEMORY="$2"
      shift 2
      ;;
    --cores)
      CORES="$2"
      shift 2
      ;;
    -h|--help)
      grep '^# ' "$0" | cut -c 3-
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

if ! command -v qm >/dev/null 2>&1; then
  echo "Error: 'qm' command not found. This script must be run directly on a Proxmox VE host." >&2
  exit 1
fi

if qm status "${VMID}" >/dev/null 2>&1; then
  echo "Error: VM ID ${VMID} already exists on this Proxmox node." >&2
  exit 1
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

case "${OS}" in
  debian-12)
    IMAGE_URL="https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2"
    IMAGE_FILE="${TMP_DIR}/debian-12-genericcloud-amd64.qcow2"
    ;;
  ubuntu-24.04)
    IMAGE_URL="https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
    IMAGE_FILE="${TMP_DIR}/noble-server-cloudimg-amd64.img"
    ;;
  *)
    echo "Unsupported OS '${OS}'. Supported: debian-12, ubuntu-24.04" >&2
    exit 1
    ;;
esac

echo "==> Downloading ${OS} cloud image from ${IMAGE_URL}..."
if command -v curl >/dev/null 2>&1; then
  curl -fsSL -o "${IMAGE_FILE}" "${IMAGE_URL}"
else
  wget -q -O "${IMAGE_FILE}" "${IMAGE_URL}"
fi

echo "==> Creating VM ${VMID} (${NAME})..."
qm create "${VMID}" \
  --name "${NAME}" \
  --memory "${MEMORY}" \
  --cores "${CORES}" \
  --cpu host \
  --net0 "virtio,bridge=${BRIDGE}"

echo "==> Importing disk image into storage '${STORAGE}'..."
qm importdisk "${VMID}" "${IMAGE_FILE}" "${STORAGE}"

echo "==> Configuring VM devices and Cloud-Init..."
qm set "${VMID}" \
  --scsihw virtio-scsi-single \
  --scsi0 "${STORAGE}:vm-${VMID}-disk-0" \
  --boot c --bootdisk scsi0 \
  --ide2 "${STORAGE}:cloudinit" \
  --serial0 socket \
  --vga serial0 \
  --agent enabled=1

echo "==> Converting VM ${VMID} to template..."
qm template "${VMID}"

echo "==> Template '${NAME}' (VMID ${VMID}) successfully created on storage '${STORAGE}'!"
