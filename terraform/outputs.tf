output "server_ip" {
  description = "Public IP of the k3s node."
  value       = hcloud_server.k3s.ipv4_address
}

output "gateway_url" {
  description = "Gateway endpoint for k6 tests."
  value       = "http://${hcloud_server.k3s.ipv4_address}:30080"
}

output "grafana_url" {
  description = "Grafana endpoint (after deploying monitoring.yaml)."
  value       = "http://${hcloud_server.k3s.ipv4_address}:30030"
}

output "prometheus_url" {
  description = "Prometheus endpoint."
  value       = "http://${hcloud_server.k3s.ipv4_address}:30090"
}

output "kubeconfig_command" {
  description = "Command to download the kubeconfig from the server."
  value       = "scp root@${hcloud_server.k3s.ipv4_address}:/root/kubeconfig.yaml ~/.kube/rate-limiter-k3s.yaml"
}

output "github_secret_command" {
  description = "Command to base64-encode the kubeconfig for the KUBECONFIG_CLOUD GitHub secret."
  value       = "cat ~/.kube/rate-limiter-k3s.yaml | base64 | pbcopy  # macOS — paste into GitHub secret"
}
