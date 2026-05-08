terraform {
  required_version = ">= 1.6"
  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = "~> 1.47"
    }
  }
}

provider "hcloud" {
  token = var.hcloud_token
}

# ── SSH key ────────────────────────────────────────────────────────────────────

resource "hcloud_ssh_key" "default" {
  name       = "rate-limiter-key"
  public_key = var.ssh_public_key
}

# ── Firewall ───────────────────────────────────────────────────────────────────

resource "hcloud_firewall" "k3s" {
  name = "rate-limiter-k3s"

  # SSH
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "22"
    source_ips = ["0.0.0.0/0", "::/0"]
  }

  # k3s API server (needed for kubectl and GitHub Actions)
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "6443"
    source_ips = ["0.0.0.0/0", "::/0"]
  }

  # Gateway NodePort
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "30080"
    source_ips = ["0.0.0.0/0", "::/0"]
  }

  # Prometheus NodePort
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "30090"
    source_ips = ["0.0.0.0/0", "::/0"]
  }

  # Grafana NodePort
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "30030"
    source_ips = ["0.0.0.0/0", "::/0"]
  }

  rule {
    direction  = "in"
    protocol   = "icmp"
    source_ips = ["0.0.0.0/0", "::/0"]
  }
}

# ── VPS + k3s via cloud-init ───────────────────────────────────────────────────

resource "hcloud_server" "k3s" {
  name         = "rate-limiter-k3s"
  image        = "ubuntu-22.04"
  server_type  = var.server_type
  location     = var.location
  ssh_keys     = [hcloud_ssh_key.default.id]
  firewall_ids = [hcloud_firewall.k3s.id]

  # cloud-init installs k3s on first boot.
  # --disable=traefik: we use NodePort directly, no Ingress controller needed.
  # --tls-san: adds the public IP to the k3s TLS cert so kubectl works remotely.
  user_data = <<-EOT
    #!/bin/bash
    set -euo pipefail
    export DEBIAN_FRONTEND=noninteractive

    apt-get update -qq
    apt-get install -y -qq curl

    # Install k3s — single node, no traefik
    curl -sfL https://get.k3s.io | \
      INSTALL_K3S_EXEC="server --disable=traefik --tls-san=$(curl -s ifconfig.me)" \
      sh -

    # Wait until the node is Ready
    until kubectl get nodes 2>/dev/null | grep -q " Ready"; do
      sleep 2
    done

    # Export kubeconfig with the public IP so it works from outside the server.
    PUBLIC_IP=$(curl -s ifconfig.me)
    sed "s/127.0.0.1/$PUBLIC_IP/g" /etc/rancher/k3s/k3s.yaml > /root/kubeconfig.yaml
    chmod 600 /root/kubeconfig.yaml

    echo "k3s ready. Kubeconfig saved to /root/kubeconfig.yaml"
  EOT
}
