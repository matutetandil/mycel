terraform {
  required_providers {
    linode = {
      source  = "linode/linode"
      version = "~> 2.0"
    }
  }
}

provider "linode" {
  token = var.linode_token
}

# --------------------------------------------------------------------------
# Variables
# --------------------------------------------------------------------------

variable "linode_token" {
  description = "Linode API token"
  type        = string
  sensitive   = true
}

variable "region" {
  description = "Linode region"
  type        = string
  default     = "us-east"
}

variable "plan" {
  description = "Linode plan for targets and database (nanode = $5/mo)"
  type        = string
  default     = "g6-nanode-1" # 1 vCPU, 1GB RAM, $5/mo
}

variable "attacker_plan" {
  description = "Linode plan for the attacker VPS (needs more CPU for parallel k6)"
  type        = string
  default     = "g6-standard-2" # 4 vCPU, 4GB RAM, $24/mo
}

variable "ssh_public_key" {
  description = "Path to SSH public key"
  type        = string
  default     = "~/.ssh/id_rsa.pub"
}

variable "mycel_image" {
  description = "Mycel container image the targets run. Pin a version so a result can be attributed to one."
  type        = string
  default     = "ghcr.io/matutetandil/mycel:latest"
}

variable "deploy_attacker" {
  description = "Deploy attacker VPS to run load tests from (true = remote attack, false = local attack)"
  type        = bool
  default     = false
}

# --------------------------------------------------------------------------
# SSH Key (read from local filesystem)
# --------------------------------------------------------------------------

data "local_file" "ssh_pub_key" {
  filename = pathexpand(var.ssh_public_key)
}

resource "linode_sshkey" "benchmark" {
  label   = "mycel-benchmark"
  ssh_key = trimspace(data.local_file.ssh_pub_key.content)
}

# --------------------------------------------------------------------------
# StackScripts
# --------------------------------------------------------------------------

resource "linode_stackscript" "setup_target_standard" {
  label       = "mycel-bench-target-standard"
  description = "Setup Mycel benchmark target (standard, no DB)"
  images      = ["linode/ubuntu24.04"]
  script      = file("${path.module}/scripts/setup-target-standard.sh")
}

resource "linode_stackscript" "setup_target_db" {
  label       = "mycel-bench-target-db"
  description = "Setup Mycel benchmark target (with PostgreSQL)"
  images      = ["linode/ubuntu24.04"]
  script      = file("${path.module}/scripts/setup-target-db.sh")
}

resource "linode_stackscript" "setup_database" {
  label       = "mycel-benchmark-database-setup"
  description = "Setup PostgreSQL for benchmark"
  images      = ["linode/ubuntu24.04"]
  script      = file("${path.module}/scripts/setup-database.sh")
}

resource "linode_stackscript" "setup_attacker" {
  label       = "mycel-benchmark-attacker-setup"
  description = "Setup k6 load testing"
  images      = ["linode/ubuntu24.04"]
  script      = file("${path.module}/scripts/setup-attacker.sh")
}

# --------------------------------------------------------------------------
# Database VPS (PostgreSQL) -- created first, targets depend on its IP
# --------------------------------------------------------------------------

resource "linode_instance" "database" {
  label           = "mycel-bench-database"
  region          = var.region
  type            = var.plan
  image           = "linode/ubuntu24.04"
  authorized_keys = [linode_sshkey.benchmark.ssh_key]

  stackscript_id   = linode_stackscript.setup_database.id
  stackscript_data = {}
}

# --------------------------------------------------------------------------
# Target VPS: Standard (no DB, transform-only flows)
# --------------------------------------------------------------------------

resource "linode_instance" "target_standard" {
  label           = "mycel-bench-standard"
  region          = var.region
  type            = var.plan
  image           = "linode/ubuntu24.04"
  authorized_keys = [linode_sshkey.benchmark.ssh_key]

  stackscript_id = linode_stackscript.setup_target_standard.id
  stackscript_data = {
    "mycel_image" = var.mycel_image
  }
}

# --------------------------------------------------------------------------
# Target VPS: Realistic (with DB connector, CRUD flows)
# --------------------------------------------------------------------------

resource "linode_instance" "target_realistic" {
  label           = "mycel-bench-realistic"
  region          = var.region
  type            = var.plan
  image           = "linode/ubuntu24.04"
  authorized_keys = [linode_sshkey.benchmark.ssh_key]

  stackscript_id = linode_stackscript.setup_target_db.id
  stackscript_data = {
    "database_ip" = tolist(linode_instance.database.ipv4)[0]
    "mycel_image" = var.mycel_image
  }
}

# --------------------------------------------------------------------------
# Target VPS: Stress (with DB connector, CRUD flows)
# --------------------------------------------------------------------------

resource "linode_instance" "target_stress" {
  label           = "mycel-bench-stress"
  region          = var.region
  type            = var.plan
  image           = "linode/ubuntu24.04"
  authorized_keys = [linode_sshkey.benchmark.ssh_key]

  stackscript_id = linode_stackscript.setup_target_db.id
  stackscript_data = {
    "database_ip" = tolist(linode_instance.database.ipv4)[0]
    "mycel_image" = var.mycel_image
  }
}

# --------------------------------------------------------------------------
# Attacker VPS (optional -- for remote load testing)
# --------------------------------------------------------------------------

resource "linode_instance" "attacker" {
  count           = var.deploy_attacker ? 1 : 0
  label           = "mycel-bench-attacker"
  region          = var.region
  type            = var.attacker_plan
  image           = "linode/ubuntu24.04"
  authorized_keys = [linode_sshkey.benchmark.ssh_key]

  stackscript_id = linode_stackscript.setup_attacker.id
  stackscript_data = {
    "target_ip" = tolist(linode_instance.target_standard.ipv4)[0]
  }
}

# --------------------------------------------------------------------------
# Outputs
# --------------------------------------------------------------------------

output "target_standard_ip" {
  value       = tolist(linode_instance.target_standard.ipv4)[0]
  description = "IP of the standard benchmark target VPS"
}

output "target_realistic_ip" {
  value       = tolist(linode_instance.target_realistic.ipv4)[0]
  description = "IP of the realistic benchmark target VPS"
}

output "target_stress_ip" {
  value       = tolist(linode_instance.target_stress.ipv4)[0]
  description = "IP of the stress test target VPS"
}

output "database_ip" {
  value       = tolist(linode_instance.database.ipv4)[0]
  description = "IP of the PostgreSQL VPS"
}

output "mycel_image" {
  value       = var.mycel_image
  description = "Image the targets were deployed with"
}

output "attacker_ip" {
  value       = var.deploy_attacker ? tolist(linode_instance.attacker[0].ipv4)[0] : "local"
  description = "IP of the attacker VPS (or 'local' if running from your machine)"
}

output "ssh_target_standard" {
  value = "ssh root@${tolist(linode_instance.target_standard.ipv4)[0]}"
}

output "ssh_target_realistic" {
  value = "ssh root@${tolist(linode_instance.target_realistic.ipv4)[0]}"
}

output "ssh_target_stress" {
  value = "ssh root@${tolist(linode_instance.target_stress.ipv4)[0]}"
}

output "ssh_attacker" {
  value = var.deploy_attacker ? "ssh root@${tolist(linode_instance.attacker[0].ipv4)[0]}" : "n/a (running locally)"
}
