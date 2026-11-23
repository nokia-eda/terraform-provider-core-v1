data "core-v1_auth_users" "all-users" {
}

# Root password supplied at runtime, never stored in plan or state.
# Requires Terraform 1.11+ / OpenTofu 1.11+ (ephemeral variables, write-only attributes).
variable "new_user_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "core-v1_auth_user_group" "operators" {
  name        = "operators"
  description = "Operators"
}

# New non-admin user. Do not manage the built-in admin user this way.
resource "core-v1_auth_user" "new-user" {
  username   = "newuser"
  first_name = "new"
  last_name  = "user"
  email      = "newuser@eda.nokia.com"

  # Full desired membership; omit to leave membership unmanaged, [] to clear it.
  groups = [core-v1_auth_user_group.operators.uuid]

  # Write-only password: applied on create and when the version is increased.
  password_wo         = var.new_user_password
  password_wo_version = 1
}

# import {
#   to = core-v1_auth_user.admin
#   id = "f2a75035-56a5-4ba0-be9b-53e1351259be"
# }

# resource "core-v1_auth_user" "admin" {
# }
