#!/bin/bash
# Cloud-init — runs once on first boot automatically.
# Installs Docker, clones the repo, starts the stack.

set -euo pipefail
exec > /var/log/user_data.log 2>&1
echo "=== cloud-init start $(date) ==="

# ── 1. System update ───────────────────────────────────────────────────────
apt-get update -y
apt-get install -y ca-certificates curl git unzip

# ── 2. Install Docker ──────────────────────────────────────────────────────
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
  -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc

echo \
  "deb [arch=$(dpkg --print-architecture) \
  signed-by=/etc/apt/keyrings/docker.asc] \
  https://download.docker.com/linux/ubuntu \
  $(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
  > /etc/apt/sources.list.d/docker.list

apt-get update -y
apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

systemctl enable docker
systemctl start docker

echo "=== Docker installed: $(docker --version) ==="

# ── 3. Clone the project ───────────────────────────────────────────────────
# Change this to your actual repo URL before running terraform apply.
REPO_URL="${repo_url}"
APP_DIR="/opt/${project_name}"

git clone "$REPO_URL" "$APP_DIR"
cd "$APP_DIR"

# ── 4. Configure environment ───────────────────────────────────────────────
cp .env.example .env

# ── 5. Start the stack ─────────────────────────────────────────────────────
docker compose up -d --build

echo "=== Stack started $(date) ==="
echo "=== Containers: ==="
docker compose ps
