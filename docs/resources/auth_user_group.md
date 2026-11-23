---
# NOTE: hand-authored from the patched custom schema (internal/resource_auth_user_group/auth_user_group_custom.go); tfplugindocs was not run.
page_title: "core-v1_auth_user_group Resource - core-v1"
subcategory: ""
description: |-
  EDA user group with native role binding.
---

# core-v1_auth_user_group (Resource)

Manages an EDA user group. Name and description use `POST/PUT /core/admin/groups`; roles use
`PUT /core/admin/groups/{uuid}/roles`, because EDA rejects `roles` on create
(`CannotSpecifyRolesOnCreate`) and silently ignores them on `PUT /core/admin/groups/{uuid}`.

> **Local unreleased patch.** See [auth_user_group patch notes](../auth_user_group_patch_notes.md),
> including the breaking changes.

## Roles

`roles` is the **full** set of roles bound to the group: `<namespace>:<name>` for a Role,
`<name>` for a ClusterRole. Roles bound outside Terraform are removed on the next apply; omit
nothing and use `[]` (the default) for a group without roles. A role must exist before it is
bound (EDA answers `404 RoleNotFound`): reference the role resource's name so the order is right.

## Membership

Membership belongs to `core-v1_auth_user.groups`. `users` is read-only here and the provider never
sends it.

## Built-in group is protected

The built-in `system-administrator` group is never created, imported, read, updated or deleted by
this resource (the guard checks the actual name returned by the API).

## Example

```terraform
resource "core-v1_auth_user_group" "operators" {
  name        = "eda-operators"
  description = "EDA operators"
  roles       = ["eda:network-operator"]
}
```

## Schema

### Required

- `name` (String) Group name. Changing it replaces the group.

### Optional

- `description` (String) Defaults to `""`.
- `roles` (Set of String) Full set of bound roles. Defaults to `[]`.

### Read-Only

- `users` (Set of String) Usernames of the members.
- `uuid` (String) The UUID of the group.

## Import

```shell
terraform import core-v1_auth_user_group.operators <group uuid>
```
