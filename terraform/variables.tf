variable "hcloud_token" {
  description = "Hetzner Cloud API token. Create one at https://console.hetzner.cloud → project → Security → API Tokens."
  type        = string
  sensitive   = true
}

variable "ssh_public_key" {
  description = "SSH public key content (e.g. the contents of ~/.ssh/id_ed25519.pub)."
  type        = string
}

variable "server_type" {
  description = "Hetzner server type. cx21 (2 vCPU, 4 GB, ~€4/mo) is enough for the blog post experiments."
  type        = string
  default     = "cx21"
}

variable "location" {
  description = "Hetzner datacenter location."
  type        = string
  default     = "nbg1"   # Nuremberg — lowest latency from most of Europe
}
