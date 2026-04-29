terraform {
  required_version = ">= 1.6"
  required_providers {
    digitalocean = {
      source  = "digitalocean/digitalocean"
      version = "~> 2.39"
    }
  }
}

provider "digitalocean" {
  token = var.do_token
}

# ── SSH key ────────────────────────────────────────────────────────────────
resource "digitalocean_ssh_key" "default" {
  name       = "${var.project_name}-key"
  public_key = file(var.ssh_public_key_path)
}

# ── Droplet ────────────────────────────────────────────────────────────────
resource "digitalocean_droplet" "app" {
  name   = var.project_name
  region = var.region
  size   = var.droplet_size
  image  = "ubuntu-24-04-x64"

  ssh_keys = [digitalocean_ssh_key.default.fingerprint]

  # Runs once on first boot: installs Docker, clones repo, starts stack.
  user_data = templatefile("${path.module}/user_data.sh.tpl", {
    project_name = var.project_name
    repo_url     = var.repo_url
  })

  tags = ["sre-project", "assignment5"]
}

# ── Firewall ───────────────────────────────────────────────────────────────
resource "digitalocean_firewall" "app" {
  name        = "${var.project_name}-fw"
  droplet_ids = [digitalocean_droplet.app.id]

  # SSH
  inbound_rule {
    protocol         = "tcp"
    port_range       = "22"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }
  # Frontend (Nginx)
  inbound_rule {
    protocol         = "tcp"
    port_range       = "80"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }
  # Grafana
  inbound_rule {
    protocol         = "tcp"
    port_range       = "3000"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }
  # Prometheus
  inbound_rule {
    protocol         = "tcp"
    port_range       = "9090"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }

  # Outbound — всё разрешено
  outbound_rule {
    protocol              = "tcp"
    port_range            = "1-65535"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }
  outbound_rule {
    protocol              = "udp"
    port_range            = "1-65535"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }
  outbound_rule {
    protocol              = "icmp"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }
}
