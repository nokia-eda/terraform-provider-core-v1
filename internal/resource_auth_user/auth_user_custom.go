package resource_auth_user

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// This file is hand written (NOT generated) so that regenerating
// auth_user_resource_gen.go cannot overwrite the user management fixes.

var uuidRegexp = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IsUUID reports whether s looks like a canonical UUID.
func IsUUID(s string) bool { return uuidRegexp.MatchString(s) }

// AuthUserCustomModel is the Terraform model of the custom auth_user resource.
type AuthUserCustomModel struct {
	Username          types.String `tfsdk:"username"`
	Email             types.String `tfsdk:"email"`
	FirstName         types.String `tfsdk:"first_name"`
	LastName          types.String `tfsdk:"last_name"`
	Enabled           types.Bool   `tfsdk:"enabled"`
	MaxSessions       types.Int64  `tfsdk:"max_sessions"`
	Groups            types.List   `tfsdk:"groups"`
	Status            types.Object `tfsdk:"status"`
	Uuid              types.String `tfsdk:"uuid"`
	Password          types.String `tfsdk:"password"`
	PasswordWo        types.String `tfsdk:"password_wo"`
	PasswordWoVersion types.Int64  `tfsdk:"password_wo_version"`
}

// StatusAttrTypes are the attribute types of the computed `status` object.
var StatusAttrTypes = map[string]attr.Type{
	"failed_login_since_successful_login": types.Int64Type,
	"federation_provider_name":            types.StringType,
	"federation_provider_uuid":            types.StringType,
	"is_federated_user":                   types.BoolType,
	"last_failed_login":                   types.StringType,
	"last_successful_login":               types.StringType,
	"temporarily_disabled":                types.BoolType,
}

func AuthUserCustomResourceSchema(ctx context.Context) schema.Schema {
	return schema.Schema{
		Description: "EDA user. Group membership and the password are managed through the dedicated EDA " +
			"membership / resetpassword APIs. The password is write-only: it is never stored in plan or state.",
		Attributes: map[string]schema.Attribute{
			"username": schema.StringAttribute{
				Required:      true,
				Description:   "User name. Changing it replaces the user.",
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"email": schema.StringAttribute{
				Required:    true,
				Description: "Email address (required by the EDA API).",
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"first_name": schema.StringAttribute{
				Required:    true,
				Description: "First name (required by the EDA API).",
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"last_name": schema.StringAttribute{
				Required:    true,
				Description: "Last name (required by the EDA API).",
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the user is enabled. Defaults to true.",
			},
			"max_sessions": schema.Int64Attribute{
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"groups": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Description: "UUIDs of the groups the user is a member of. When configured, this is the full " +
					"desired membership (an empty list removes the user from all groups). When omitted, " +
					"membership is not managed by Terraform.",
				MarkdownDescription: "UUIDs of the groups the user is a member of. When configured, this is the full " +
					"desired membership (an empty list removes the user from all groups). When omitted, " +
					"membership is not managed by Terraform.",
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"status": schema.SingleNestedAttribute{
				Computed:    true,
				Description: "Read-only login status of the user.",
				Attributes: map[string]schema.Attribute{
					"failed_login_since_successful_login": schema.Int64Attribute{Computed: true},
					"federation_provider_name":            schema.StringAttribute{Computed: true},
					"federation_provider_uuid":            schema.StringAttribute{Computed: true},
					"is_federated_user":                   schema.BoolAttribute{Computed: true},
					"last_failed_login":                   schema.StringAttribute{Computed: true},
					"last_successful_login":               schema.StringAttribute{Computed: true},
					"temporarily_disabled":                schema.BoolAttribute{Computed: true},
				},
			},
			"uuid": schema.StringAttribute{
				Computed:      true,
				Description:   "The UUID of the user.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"password": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				DeprecationMessage: "The plaintext `password` attribute is no longer supported and is never sent. " +
					"Use `password_wo` together with `password_wo_version` instead.",
				Description: "Removed. Use password_wo + password_wo_version.",
				Validators:  []validator.String{rejectLegacyPassword{}},
			},
			"password_wo": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				WriteOnly: true,
				Description: "Write-only password. Never stored in plan or state. Applied on create and whenever " +
					"password_wo_version changes. Requires password_wo_version.",
				MarkdownDescription: "Write-only password. Never stored in plan or state. Applied on create and whenever " +
					"`password_wo_version` changes. Requires `password_wo_version`.",
			},
			"password_wo_version": schema.Int64Attribute{
				Optional: true,
				Description: "Version of the write-only password. Increase it to rotate the password. " +
					"Required when password_wo is set. Not set after import until an intentional reset.",
				MarkdownDescription: "Version of the write-only password. Increase it to rotate the password. " +
					"Required when `password_wo` is set. Not set after import until an intentional reset.",
				Validators: []validator.Int64{int64validator.AtLeast(1)},
			},
		},
	}
}

type rejectLegacyPassword struct{}

func (rejectLegacyPassword) Description(context.Context) string {
	return "rejects the removed plaintext password attribute"
}
func (v rejectLegacyPassword) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (rejectLegacyPassword) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() {
		return
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Plaintext password is not supported",
		"The `password` attribute is no longer supported because it would be stored in state. "+
			"Use `password_wo` (write-only) together with `password_wo_version` instead.")
}

// ValidateModel performs cross attribute validation of a configuration model.
// It returns (summary, detail) pairs for each problem.
func ValidateModel(m *AuthUserCustomModel) [][2]string {
	var errs [][2]string
	if !m.Username.IsNull() && !m.Username.IsUnknown() {
		switch strings.ToLower(strings.TrimSpace(m.Username.ValueString())) {
		case "admin", "system-administrator":
			errs = append(errs, [2]string{"Protected user",
				"built-in users (admin, system-administrator) are not managed by core-v1_auth_user; use the platform bootstrap/admin lifecycle."})
		}
	}
	pwKnown := !m.PasswordWo.IsUnknown()
	verKnown := !m.PasswordWoVersion.IsUnknown()
	if pwKnown && verKnown {
		if !m.PasswordWo.IsNull() && m.PasswordWoVersion.IsNull() {
			errs = append(errs, [2]string{"Missing password_wo_version",
				"`password_wo_version` must be set whenever `password_wo` is set."})
		}
		if m.PasswordWo.IsNull() && !m.PasswordWoVersion.IsNull() {
			errs = append(errs, [2]string{"Missing password_wo",
				"`password_wo` must be set whenever `password_wo_version` is set."})
		}
	}
	if !m.Groups.IsNull() && !m.Groups.IsUnknown() {
		seen := map[string]bool{}
		for _, e := range m.Groups.Elements() {
			s, ok := e.(types.String)
			if !ok || s.IsUnknown() {
				continue
			}
			if s.IsNull() || strings.TrimSpace(s.ValueString()) == "" {
				errs = append(errs, [2]string{"Invalid group", "`groups` must not contain null or empty UUIDs."})
				continue
			}
			if seen[s.ValueString()] {
				errs = append(errs, [2]string{"Duplicate group", fmt.Sprintf("group %q is listed more than once in `groups`.", s.ValueString())})
			}
			seen[s.ValueString()] = true
		}
	}
	return errs
}
