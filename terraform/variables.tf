variable "do_token" {
  description = "DigitalOcean personal access token (из https://cloud.digitalocean.com/account/api/tokens)"
  type        = string
  sensitive   = true
}

variable "project_name" {
  description = "Имя дроплета и префикс ресурсов"
  type        = string
  default     = "sre-microservices"
}

variable "region" {
  description = "Регион DigitalOcean"
  type        = string
  default     = "fra1"
}

variable "droplet_size" {
  description = "Размер дроплета. s-2vcpu-4gb хватает для всех 9 контейнеров."
  type        = string
  default     = "s-2vcpu-2gb"
}

variable "ssh_public_key_path" {
  description = "Путь до публичного SSH ключа на твоей машине"
  type        = string
  default     = "~/.ssh/id_rsa.pub"
}

variable "repo_url" {
  description = "URL твоего git репозитория с проектом"
  type        = string
  # Пример: "https://github.com/username/sre-project.git"
}
