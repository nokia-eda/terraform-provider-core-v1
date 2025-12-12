data "core-v1_auth_users" "all-users" {
}

resource "core-v1_auth_user" "new-user" {
  username   = "newuser"
  first_name = "new"
  last_name  = "user"
  email      = "newuser@eda.nokia.com"
}

# import {
#   to = core-v1_auth_user.admin
#   id = "f2a75035-56a5-4ba0-be9b-53e1351259be"
# }

# resource "core-v1_auth_user" "admin" {
# }
