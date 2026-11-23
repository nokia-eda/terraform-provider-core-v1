package resource_auth_user_group

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// This file is hand written (NOT generated) so that regenerating
// auth_user_group_resource_gen.go cannot overwrite the group management fix.
//
// EDA's group API ignores `roles` on PUT /core/admin/groups/{uuid} and rejects
// them on POST (CannotSpecifyRolesOnCreate). Roles are bound through
// PUT /core/admin/groups/{uuid}/roles. Membership belongs to core-v1_auth_user.

var uuidRegexp = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IsUUID reports whether s looks like a canonical UUID.
func IsUUID(s string) bool { return uuidRegexp.MatchString(s) }

// GroupModel is the Terraform model of the custom auth_user_group resource.
type GroupModel struct {
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Roles       types.Set    `tfsdk:"roles"`
	Users       types.Set    `tfsdk:"users"`
	Uuid        types.String `tfsdk:"uuid"`
}

// IsProtectedGroup reports whether name is a built-in group this resource must never manage.
func IsProtectedGroup(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "system-administrator")
}

// GroupSchema is the schema of the custom auth_user_group resource.
func GroupSchema(ctx context.Context) schema.Schema {
	return schema.Schema{
		Description: "EDA user group. Roles are bound through the dedicated EDA roles API; membership is owned by " +
			"core-v1_auth_user (`groups`) and only read back here.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Group name. Changing it replaces the group. The built-in `system-administrator` group is refused.",
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Description: "Group description.",
			},
			"roles": schema.SetAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Default:     setdefault.StaticValue(types.SetValueMust(types.StringType, []attr.Value{})),
				Description: "Roles bound to the group: `<namespace>:<name>` for a Role, `<name>` for a ClusterRole. " +
					"This is the full set: roles bound outside Terraform are removed. The roles must already exist.",
			},
			"users": schema.SetAttribute{
				ElementType:   types.StringType,
				Computed:      true,
				Description:   "Usernames of the members (read-only; set `groups` on core-v1_auth_user).",
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"uuid": schema.StringAttribute{
				Computed:      true,
				Description:   "The UUID of the group.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// ValidateGroupModel performs cross attribute validation of a configuration model.
// It returns (summary, detail) pairs for each problem.
func ValidateGroupModel(m *GroupModel) [][2]string {
	var errs [][2]string
	if !m.Name.IsNull() && !m.Name.IsUnknown() && IsProtectedGroup(m.Name.ValueString()) {
		errs = append(errs, [2]string{"Protected group",
			"the built-in group system-administrator is not managed by core-v1_auth_user_group."})
	}
	if !m.Roles.IsNull() && !m.Roles.IsUnknown() {
		for _, e := range m.Roles.Elements() {
			s, ok := e.(types.String)
			if !ok || s.IsUnknown() {
				continue
			}
			if s.IsNull() || strings.TrimSpace(s.ValueString()) == "" {
				errs = append(errs, [2]string{"Invalid role", "`roles` must not contain null or empty strings."})
			} else if strings.Count(s.ValueString(), ":") > 1 {
				errs = append(errs, [2]string{"Invalid role",
					fmt.Sprintf("role %q must be <namespace>:<name> (Role) or <name> (ClusterRole).", s.ValueString())})
			}
		}
	}
	return errs
}
