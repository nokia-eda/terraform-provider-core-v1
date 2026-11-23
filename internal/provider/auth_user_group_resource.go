package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/eda/apiclient"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/eda/rest"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/resource_auth_user_group"
)

const (
	create_rs_authUserGroup = "/core/admin/groups"
	read_rs_authUserGroup   = "/core/admin/groups/{uuid}"
	update_rs_authUserGroup = "/core/admin/groups/{uuid}"
	delete_rs_authUserGroup = "/core/admin/groups/{uuid}"
	roles_rs_authUserGroup  = "/core/admin/groups/{uuid}/roles"
)

var (
	_ resource.Resource                   = (*authUserGroupResource)(nil)
	_ resource.ResourceWithConfigure      = (*authUserGroupResource)(nil)
	_ resource.ResourceWithImportState    = (*authUserGroupResource)(nil)
	_ resource.ResourceWithValidateConfig = (*authUserGroupResource)(nil)
)

func NewAuthUserGroupResource() resource.Resource {
	return &authUserGroupResource{}
}

type authUserGroupResource struct {
	client authUserAPI // same Execute-only interface as the user resource
}

func (r *authUserGroupResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_auth_user_group"
}

func (r *authUserGroupResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resource_auth_user_group.GroupSchema(ctx)
}

func (r *authUserGroupResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data resource_auth_user_group.GroupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for _, e := range resource_auth_user_group.ValidateGroupModel(&data) {
		resp.Diagnostics.AddError(e[0], e[1])
	}
}

func (r *authUserGroupResource) call(ctx context.Context, method, pathUrl, uuid string, body, result any) error {
	var params map[string]string
	if uuid != "" {
		params = map[string]string{"uuid": uuid}
	}
	tflog.Debug(ctx, "authUserGroup API call", map[string]any{"method": method, "path": pathUrl})
	return r.client.Execute(ctx, pathUrl, method, params, nil, body, result)
}

// groupBody is the explicit whitelist sent to the create / update APIs:
// never roles (rejected on create, ignored on update) and never users.
func groupBody(m *resource_auth_user_group.GroupModel) map[string]any {
	desc := ""
	if !m.Description.IsNull() && !m.Description.IsUnknown() {
		desc = m.Description.ValueString()
	}
	return map[string]any{"name": m.Name.ValueString(), "description": desc}
}

func strSlice(v any) []string {
	out := []string{}
	if arr, ok := v.([]any); ok {
		for _, e := range arr {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	}
	sort.Strings(out)
	return out
}

func setOf(vals []string) types.Set {
	elems := make([]attr.Value, len(vals))
	for i, v := range vals {
		elems[i] = types.StringValue(v)
	}
	return types.SetValueMust(types.StringType, elems)
}

func setStrings(s types.Set) ([]string, bool) {
	if s.IsNull() || s.IsUnknown() {
		return nil, false
	}
	out := []string{}
	for _, e := range s.Elements() {
		if v, ok := e.(types.String); ok && !v.IsNull() && !v.IsUnknown() {
			out = append(out, v.ValueString())
		}
	}
	sort.Strings(out)
	return out, true
}

func applyGroupResponse(m *resource_auth_user_group.GroupModel, result map[string]any) {
	m.Uuid = strFrom(result["uuid"])
	m.Name = strFrom(result["name"])
	if d, ok := result["description"].(string); ok {
		m.Description = types.StringValue(d)
	} else {
		m.Description = types.StringValue("")
	}
	m.Roles = setOf(strSlice(result["roles"]))
	m.Users = setOf(strSlice(result["users"]))
}

func (r *authUserGroupResource) getGroup(ctx context.Context, uuid string) (map[string]any, error) {
	result := map[string]any{}
	if err := r.call(ctx, rest.HTTP_GET, read_rs_authUserGroup, uuid, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// checkActualGroup fails closed on a group with no identifiable name or a built-in one.
func checkActualGroup(got map[string]any) error {
	name, _ := got["name"].(string)
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("refusing to manage a group whose actual name cannot be determined")
	}
	if resource_auth_user_group.IsProtectedGroup(name) {
		return fmt.Errorf("refusing to manage the built-in group %q: it is owned by the platform bootstrap", name)
	}
	return nil
}

func (r *authUserGroupResource) guardActual(ctx context.Context, uuid string) (map[string]any, error) {
	got, err := r.getGroup(ctx, uuid)
	if err != nil {
		return nil, err
	}
	return got, checkActualGroup(got)
}

func (r *authUserGroupResource) setRoles(ctx context.Context, uuid string, roles []string) error {
	if roles == nil {
		roles = []string{}
	}
	return r.call(ctx, rest.HTTP_PUT, roles_rs_authUserGroup, uuid, roles, nil)
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (r *authUserGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan resource_auth_user_group.GroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if resource_auth_user_group.IsProtectedGroup(plan.Name.ValueString()) {
		resp.Diagnostics.AddError("Protected group", "the built-in group system-administrator cannot be managed by core-v1_auth_user_group")
		return
	}

	result := map[string]any{}
	if err := r.call(ctx, rest.HTTP_POST, create_rs_authUserGroup, "", groupBody(&plan), &result); err != nil {
		resp.Diagnostics.AddError("Error creating group", err.Error())
		return
	}
	uuid, _ := result["uuid"].(string)
	if uuid == "" {
		resp.Diagnostics.AddError("Error creating group", "the API response did not contain a group uuid")
		return
	}

	// Persist partial state (with the UUID) before the roles call so a failure
	// leaves a tainted resource that is cleaned up.
	state := plan
	applyGroupResponse(&state, result)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if desired, ok := setStrings(plan.Roles); ok && len(desired) > 0 {
		if err := r.setRoles(ctx, uuid, desired); err != nil {
			resp.Diagnostics.AddError("Error binding roles to the group", err.Error())
			return
		}
	}

	got, err := r.getGroup(ctx, uuid)
	if err != nil {
		resp.Diagnostics.AddError("Error reading group after create", err.Error())
		return
	}
	applyGroupResponse(&state, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *authUserGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data resource_auth_user_group.GroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if data.Uuid.IsNull() || data.Uuid.IsUnknown() || data.Uuid.ValueString() == "" {
		resp.Diagnostics.AddError("Error reading group", "state has no uuid")
		return
	}
	got, err := r.getGroup(ctx, data.Uuid.ValueString())
	if err != nil {
		if apiclient.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading group", err.Error())
		return
	}
	if err := checkActualGroup(got); err != nil {
		resp.Diagnostics.AddError("Protected group", err.Error())
		return
	}
	applyGroupResponse(&data, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *authUserGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state resource_auth_user_group.GroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	uuid := state.Uuid.ValueString()
	if uuid == "" {
		resp.Diagnostics.AddError("Error updating group", "state has no uuid")
		return
	}
	current, err := r.guardActual(ctx, uuid)
	if err != nil {
		resp.Diagnostics.AddError("Refusing to update group", err.Error())
		return
	}

	// 1. description (name changes replace the group)
	curDesc, _ := current["description"].(string)
	if plan.Description.ValueString() != curDesc {
		if err := r.call(ctx, rest.HTTP_PUT, update_rs_authUserGroup, uuid, groupBody(&plan), &map[string]any{}); err != nil {
			resp.Diagnostics.AddError("Error updating group", err.Error())
			return
		}
	}

	// 2. roles
	if desired, ok := setStrings(plan.Roles); ok && !sameStrings(desired, strSlice(current["roles"])) {
		if err := r.setRoles(ctx, uuid, desired); err != nil {
			resp.Diagnostics.AddError("Error binding roles to the group", err.Error())
			return
		}
	}

	got, err := r.getGroup(ctx, uuid)
	if err != nil {
		resp.Diagnostics.AddError("Error reading group after update", err.Error())
		return
	}
	applyGroupResponse(&plan, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *authUserGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data resource_auth_user_group.GroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	uuid := data.Uuid.ValueString()
	if uuid == "" {
		return
	}
	if _, err := r.guardActual(ctx, uuid); err != nil {
		if apiclient.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Refusing to delete group", err.Error())
		return
	}
	if err := r.call(ctx, rest.HTTP_DELETE, delete_rs_authUserGroup, uuid, nil, nil); err != nil && !apiclient.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting group", err.Error())
	}
}

// Configure adds the provider configured client to the resource.
func (r *authUserGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*apiclient.EdaApiClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *api.EdaApiClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	r.client = client
}

// ImportState implements resource.ResourceWithImportState. The import ID is the group UUID.
func (r *authUserGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := strings.TrimSpace(req.ID)
	if !resource_auth_user_group.IsUUID(id) {
		resp.Diagnostics.AddError("Invalid ID", "Expected format: id = <group uuid>")
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "the API client is not configured")
		return
	}
	if _, err := r.guardActual(ctx, id); err != nil {
		resp.Diagnostics.AddError("Cannot import group", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("uuid"), id)...)
}
