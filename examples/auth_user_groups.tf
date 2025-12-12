data "core-v1_auth_user_groups" "all-usergroups" {
  fullroles = "true"
  fullusers = "true"
}

resource "core-v1_auth_user_group" "new-ug" {
  name        = "new-usergroup"
  description = "New user group description"
}

# import {
#   to = core-v1_auth_user_group.system-admin
#   id = "5aefa9bb-6459-491e-994a-794c9a04a6e3"
# }

# resource "core-v1_auth_user_group" "system-admin" {
# }
