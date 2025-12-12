output "all-user-groups" {
  value = data.core-v1_auth_user_groups.all-usergroups
}

output "all-users" {
  value = data.core-v1_auth_users.all-users
}
