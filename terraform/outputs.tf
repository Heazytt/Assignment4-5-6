output "droplet_ip" {
  description = "Public IPv4 address of the droplet"
  value       = digitalocean_droplet.app.ipv4_address
}

output "frontend_url" {
  description = "URL of the frontend"
  value       = "http://${digitalocean_droplet.app.ipv4_address}"
}

output "grafana_url" {
  description = "URL of Grafana"
  value       = "http://${digitalocean_droplet.app.ipv4_address}:3000"
}

output "prometheus_url" {
  description = "URL of Prometheus"
  value       = "http://${digitalocean_droplet.app.ipv4_address}:9090"
}

output "ssh_command" {
  description = "SSH command to connect to the droplet"
  value       = "ssh root@${digitalocean_droplet.app.ipv4_address}"
}
