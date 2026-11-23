package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/eda/apiclient"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/eda/rest"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/resource_auth_user"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/tfutils"
)

const (
	create_rs_authUser        = "/core/admin/users"
	read_rs_authUser          = "/core/admin/users/{uuid}"
	update_rs_authUser        = "/core/admin/users/{uuid}"
	delete_rs_authUser        = "/core/admin/users/{uuid}"
	groups_rs_authUser        = "/core/admin/users/{uuid}/groups"
	resetpassword_rs_authUser = "/core/admin/users/{uuid}/resetpassword"
)

var (
	_ resource.Resource                   = (*authUserResource)(nil)
	_ resource.ResourceWithConfigure      = (*authUserResource)(nil)
	_ resource.ResourceWithImportState    = (*authUserResource)(nil)
	_ resource.ResourceWithValidateConfig = (*authUserResource)(nil)
)

// authUserAPI is the subset of the EDA API client used by the user resource.
// It is satisfied by *apiclient.EdaApiClient and by fakes in tests.
type authUserAPI interface {
	Execute(ctx context.Context, pathUrl, method string, pathParams, queryParams map[string]string, body, result any) error
}

var _ authUserAPI = (*apiclient.EdaApiClient)(nil)

func NewAuthUserResource() resource.Resource {
	return &authUserResource{}
}

type authUserResource struct {
	client authUserAPI
}

func (r *authUserResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_auth_user"
}

func (r *authUserResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resource_auth_user.AuthUserCustomResourceSchema(ctx)
}

func (r *authUserResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data resource_auth_user.AuthUserCustomModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for _, e := range resource_auth_user.ValidateModel(&data) {
		resp.Diagnostics.AddError(e[0], e[1])
	}
}

// ---------------------------------------------------------------------------
// helpers (never log request bodies or models: they may carry secrets)
// ---------------------------------------------------------------------------

func (r *authUserResource) call(ctx context.Context, method, pathUrl, uuid string, body, result any) error {
	var params map[string]string
	if uuid != "" {
		params = map[string]string{"uuid": uuid}
	}
	tflog.Debug(ctx, "authUser API call", map[string]any{"method": method, "path": pathUrl})
	return r.client.Execute(ctx, pathUrl, method, params, nil, body, result)
}

// identityBody is the explicit whitelist of identity fields sent to the
// create / update APIs. Never contains password or groups.
func identityBody(m *resource_auth_user.AuthUserCustomModel) map[string]any {
	body := map[string]any{
		"username":  m.Username.ValueString(),
		"email":     m.Email.ValueString(),
		"firstName": m.FirstName.ValueString(),
		"lastName":  m.LastName.ValueString(),
		"enabled":   m.Enabled.IsNull() || m.Enabled.IsUnknown() || m.Enabled.ValueBool(),
	}
	if !m.MaxSessions.IsNull() && !m.MaxSessions.IsUnknown() {
		body["maxSessions"] = m.MaxSessions.ValueInt64()
	}
	return body
}

func strFrom(v any) types.String {
	if s, ok := v.(string); ok {
		return types.StringValue(s)
	}
	return types.StringNull()
}

func boolFrom(v any) types.Bool {
	if b, ok := v.(bool); ok {
		return types.BoolValue(b)
	}
	return types.BoolNull()
}

func intFrom(v any) types.Int64 {
	if v == nil {
		return types.Int64Null()
	}
	if n, err := tfutils.NumToInt64(v); err == nil {
		return types.Int64Value(n)
	}
	return types.Int64Null()
}

func statusFrom(v any) types.Object {
	m, _ := v.(map[string]any)
	return types.ObjectValueMust(resource_auth_user.StatusAttrTypes, map[string]attr.Value{
		"failed_login_since_successful_login": intFrom(m["failedLoginSinceSuccessfulLogin"]),
		"federation_provider_name":            strFrom(m["federationProviderName"]),
		"federation_provider_uuid":            strFrom(m["federationProviderUUID"]),
		"is_federated_user":                   boolFrom(m["isFederatedUser"]),
		"last_failed_login":                   strFrom(m["lastFailedLogin"]),
		"last_successful_login":               strFrom(m["lastSuccessfulLogin"]),
		"temporarily_disabled":                boolFrom(m["temporarilyDisabled"]),
	})
}

// groupsFromResponse extracts the group UUIDs of a user response. Absent or
// null groups are normalised to an empty list.
func groupsFromResponse(result map[string]any) []string {
	out := []string{}
	if arr, ok := result["groups"].([]any); ok {
		for _, e := range arr {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// orderLike returns preferred when it holds the same set as server (so a
// server-side reordering is not seen as drift), otherwise the server list.
func orderLike(server, preferred []string) []string {
	if preferred != nil && sameSet(server, preferred) {
		return append([]string{}, preferred...)
	}
	return server
}

func stringList(l types.List) ([]string, bool) {
	if l.IsNull() || l.IsUnknown() {
		return nil, false
	}
	out := []string{}
	for _, e := range l.Elements() {
		if s, ok := e.(types.String); ok && !s.IsNull() && !s.IsUnknown() {
			out = append(out, s.ValueString())
		}
	}
	return out, true
}

func listOf(vals []string) types.List {
	elems := make([]attr.Value, len(vals))
	for i, v := range vals {
		elems[i] = types.StringValue(v)
	}
	return types.ListValueMust(types.StringType, elems)
}

// applyUserResponse copies the API user representation into the model.
// preferred is the previously known group ordering (nil if none).
func applyUserResponse(m *resource_auth_user.AuthUserCustomModel, result map[string]any, preferred []string) {
	m.Username = strFrom(result["username"])
	m.Email = strFrom(result["email"])
	m.FirstName = strFrom(result["firstName"])
	m.LastName = strFrom(result["lastName"])
	m.Enabled = boolFrom(result["enabled"])
	m.MaxSessions = intFrom(result["maxSessions"])
	m.Status = statusFrom(result["status"])
	m.Uuid = strFrom(result["uuid"])
	m.Groups = listOf(orderLike(groupsFromResponse(result), preferred))
	// secrets never live in state
	m.Password = types.StringNull()
	m.PasswordWo = types.StringNull()
}

func (r *authUserResource) getUser(ctx context.Context, uuid string) (map[string]any, error) {
	result := map[string]any{}
	if err := r.call(ctx, rest.HTTP_GET, read_rs_authUser, uuid, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// resetPassword sets the user password. The returned error is sanitised: the
// raw HTTP response body is never included (a server or gateway may echo the
// rejected request, i.e. the password) and any occurrence of the password in
// other errors is masked.
func (r *authUserResource) resetPassword(ctx context.Context, uuid, password string) error {
	err := r.call(ctx, rest.HTTP_PUT, resetpassword_rs_authUser, uuid,
		map[string]any{"value": password, "temporary": false}, nil)
	if err == nil {
		return nil
	}
	var apiErr *apiclient.APIError
	if errors.As(err, &apiErr) {
		return fmt.Errorf("password reset rejected by the EDA API: HTTP status %d (response body omitted)", apiErr.StatusCode)
	}
	msg := err.Error()
	if password != "" {
		msg = strings.ReplaceAll(msg, password, "<redacted>")
	}
	return fmt.Errorf("password reset request failed: %s", msg)
}

// syncGroups makes the membership of the user equal to desired. The EDA API
// rejects PUT with an empty list, so clearing is done with DELETE.
func (r *authUserResource) syncGroups(ctx context.Context, uuid string, desired, current []string) error {
	if sameSet(desired, current) {
		return nil
	}
	if len(desired) == 0 {
		return r.call(ctx, rest.HTTP_DELETE, groups_rs_authUser, uuid, current, nil)
	}
	return r.call(ctx, rest.HTTP_PUT, groups_rs_authUser, uuid, desired, nil)
}

// protectedUsernames are built-in identities that this resource must never
// create, import, modify or delete: their lifecycle belongs to the platform
// bootstrap (Ansible), not to Terraform.
var protectedUsernames = map[string]bool{"admin": true, "system-administrator": true}

func isProtectedUsername(name string) bool {
	return protectedUsernames[strings.ToLower(strings.TrimSpace(name))]
}

// guardActual fetches the ACTUAL user from the API and fails closed if it is
// missing an identity or is a protected built-in user. It must be used before
// any mutation of an existing user, whatever the (possibly aliased) state says.
func (r *authUserResource) guardActual(ctx context.Context, uuid string) (map[string]any, error) {
	got, err := r.getUser(ctx, uuid)
	if err != nil {
		return nil, err
	}
	return got, checkActualIdentity(got)
}

func checkActualIdentity(got map[string]any) error {
	name, _ := got["username"].(string)
	if strings.TrimSpace(name) == "" {
		return errors.New("refusing to manage a user whose actual username cannot be determined")
	}
	if isProtectedUsername(name) {
		return fmt.Errorf("refusing to manage the built-in user %q: built-in users are not owned by core-v1_auth_user (use the platform bootstrap/admin lifecycle)", name)
	}
	return nil
}

// ensureEnabled reads the user and, if its actual enabled flag differs from
// the planned one, corrects it. The password reset API may (re-)enable an
// account, so this runs immediately after every reset, before any other
// action. The (pre-correction) readback is returned even on error so callers
// can record the truthful state.
func (r *authUserResource) ensureEnabled(ctx context.Context, uuid string, plan *resource_auth_user.AuthUserCustomModel) (map[string]any, error) {
	got, err := r.getUser(ctx, uuid)
	if err != nil {
		return nil, err
	}
	if plan.Enabled.IsNull() || plan.Enabled.IsUnknown() {
		return got, nil
	}
	if en, ok := got["enabled"].(bool); ok && en == plan.Enabled.ValueBool() {
		return got, nil
	}
	if err := r.call(ctx, rest.HTTP_PUT, update_rs_authUser, uuid, identityBody(plan), &map[string]any{}); err != nil {
		return got, fmt.Errorf("could not restore the planned enabled=%t after password reset: %w", plan.Enabled.ValueBool(), err)
	}
	fixed, err := r.getUser(ctx, uuid)
	if err != nil {
		return got, err
	}
	if en, _ := fixed["enabled"].(bool); en != plan.Enabled.ValueBool() {
		return fixed, fmt.Errorf("enabled=%t could not be enforced after password reset", plan.Enabled.ValueBool())
	}
	return fixed, nil
}

func identityChanged(plan, state *resource_auth_user.AuthUserCustomModel) bool {
	if !plan.Username.Equal(state.Username) || !plan.Email.Equal(state.Email) ||
		!plan.FirstName.Equal(state.FirstName) || !plan.LastName.Equal(state.LastName) ||
		!plan.Enabled.Equal(state.Enabled) {
		return true
	}
	if !plan.MaxSessions.IsNull() && !plan.MaxSessions.IsUnknown() && !plan.MaxSessions.Equal(state.MaxSessions) {
		return true
	}
	return false
}

// groupsManaged reports whether the configuration sets `groups`. A null config
// value means membership is unmanaged, even if the plan carries the prior
// state value (UseStateForUnknown). Unknown (deferred) config counts as managed.
func groupsManaged(ctx context.Context, cfg interface {
	GetAttribute(context.Context, path.Path, any) diag.Diagnostics
}) (bool, diag.Diagnostics) {
	var g types.List
	d := cfg.GetAttribute(ctx, path.Root("groups"), &g)
	return !g.IsNull(), d
}

func configPassword(ctx context.Context, cfg interface {
	GetAttribute(context.Context, path.Path, any) diag.Diagnostics
}) (types.String, diag.Diagnostics) {
	var pw types.String
	d := cfg.GetAttribute(ctx, path.Root("password_wo"), &pw)
	return pw, d
}

// ---------------------------------------------------------------------------
// CRUD
// ---------------------------------------------------------------------------

func (r *authUserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan resource_auth_user.AuthUserCustomModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	pw, d := configPassword(ctx, req.Config)
	resp.Diagnostics.Append(d...)
	cfgManaged, d := groupsManaged(ctx, req.Config)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !pw.IsNull() && (plan.PasswordWoVersion.IsNull() || plan.PasswordWoVersion.IsUnknown()) {
		resp.Diagnostics.AddError("Missing password_wo_version", "`password_wo_version` must be set whenever `password_wo` is set.")
		return
	}

	if isProtectedUsername(plan.Username.ValueString()) {
		resp.Diagnostics.AddError("Protected user", fmt.Sprintf("username %q is a built-in user and cannot be managed by core-v1_auth_user", plan.Username.ValueString()))
		return
	}

	// 1. identity
	result := map[string]any{}
	if err := r.call(ctx, rest.HTTP_POST, create_rs_authUser, "", identityBody(&plan), &result); err != nil {
		resp.Diagnostics.AddError("Error creating user", err.Error())
		return
	}
	uuid, _ := result["uuid"].(string)
	if uuid == "" {
		resp.Diagnostics.AddError("Error creating user", "the API response did not contain a user uuid")
		return
	}

	// Persist partial state (with the UUID) before any secondary action so a
	// failure leaves a tainted resource that can be cleaned up.
	state := plan
	applyUserResponse(&state, result, nil)
	state.PasswordWoVersion = types.Int64Null()
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// 2. password
	if !pw.IsNull() {
		if err := r.resetPassword(ctx, uuid, pw.ValueString()); err != nil {
			resp.Diagnostics.AddError("Error setting user password", err.Error())
			return
		}
		state.PasswordWoVersion = plan.PasswordWoVersion
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
		// the reset may have enabled the account: restore the planned flag
		// before anything else (fail closed, no membership on failure)
		cur, err := r.ensureEnabled(ctx, uuid, &plan)
		if cur != nil {
			if en, ok := cur["enabled"].(bool); ok {
				state.Enabled = types.BoolValue(en)
			}
		}
		if err != nil {
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			resp.Diagnostics.AddError("Error enforcing enabled flag", err.Error())
			return
		}
	}

	// 3. membership
	desired, planKnown := stringList(plan.Groups)
	managed := cfgManaged && planKnown
	if managed {
		if err := r.syncGroups(ctx, uuid, desired, groupsFromResponse(result)); err != nil {
			resp.Diagnostics.AddError("Error setting user groups", err.Error())
			return
		}
	}

	// 4. readback (and make sure the requested enabled flag took effect)
	got, err := r.ensureEnabled(ctx, uuid, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Error reading user after create", err.Error())
		return
	}
	applyUserResponse(&state, got, desired)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *authUserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data resource_auth_user.AuthUserCustomModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if data.Uuid.IsNull() || data.Uuid.IsUnknown() || data.Uuid.ValueString() == "" {
		resp.Diagnostics.AddError("Error reading user", "state has no uuid")
		return
	}
	got, err := r.getUser(ctx, data.Uuid.ValueString())
	if err != nil {
		if apiclient.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading user", err.Error())
		return
	}
	if err := checkActualIdentity(got); err != nil {
		resp.Diagnostics.AddError("Protected user", err.Error())
		return
	}
	prior, _ := stringList(data.Groups)
	applyUserResponse(&data, got, prior)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *authUserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state resource_auth_user.AuthUserCustomModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	pw, d := configPassword(ctx, req.Config)
	resp.Diagnostics.Append(d...)
	cfgManaged, d := groupsManaged(ctx, req.Config)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	uuid := state.Uuid.ValueString()
	if uuid == "" {
		resp.Diagnostics.AddError("Error updating user", "state has no uuid")
		return
	}
	if isProtectedUsername(plan.Username.ValueString()) {
		resp.Diagnostics.AddError("Protected user", "username is a built-in user and cannot be managed by core-v1_auth_user")
		return
	}
	if plan.PasswordWoVersion.IsUnknown() {
		resp.Diagnostics.AddError("Error updating user", "`password_wo_version` is unknown at apply time")
		return
	}
	rotate := !plan.PasswordWoVersion.IsNull() && !plan.PasswordWoVersion.Equal(state.PasswordWoVersion)
	if rotate && pw.IsNull() {
		resp.Diagnostics.AddError("Missing password_wo", "`password_wo` must be set when `password_wo_version` changes.")
		return
	}

	if _, err := r.guardActual(ctx, uuid); err != nil {
		resp.Diagnostics.AddError("Refusing to update user", err.Error())
		return
	}

	// 1. identity
	if identityChanged(&plan, &state) {
		result := map[string]any{}
		if err := r.call(ctx, rest.HTTP_PUT, update_rs_authUser, uuid, identityBody(&plan), &result); err != nil {
			resp.Diagnostics.AddError("Error updating user", err.Error())
			return
		}
		prior, _ := stringList(state.Groups)
		ver := state.PasswordWoVersion
		applyUserResponse(&state, result, prior)
		state.PasswordWoVersion = ver
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// 2. password (only when the version changes)
	if rotate {
		if err := r.resetPassword(ctx, uuid, pw.ValueString()); err != nil {
			resp.Diagnostics.AddError("Error setting user password", err.Error())
			return
		}
		state.PasswordWoVersion = plan.PasswordWoVersion
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
		// the reset may have (re-)enabled the account: restore the planned
		// flag immediately, before membership; fail closed on error.
		cur, err := r.ensureEnabled(ctx, uuid, &plan)
		if cur != nil {
			if en, ok := cur["enabled"].(bool); ok {
				state.Enabled = types.BoolValue(en)
			}
		}
		if err != nil {
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			resp.Diagnostics.AddError("Error enforcing enabled flag", err.Error())
			return
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	} else if plan.PasswordWoVersion.IsNull() && !state.PasswordWoVersion.IsNull() {
		state.PasswordWoVersion = types.Int64Null()
	}

	// 3. membership
	desired, planKnown := stringList(plan.Groups)
	managed := cfgManaged && planKnown
	if managed {
		current, err := r.getUser(ctx, uuid)
		if err != nil {
			resp.Diagnostics.AddError("Error reading user groups", err.Error())
			return
		}
		if err := r.syncGroups(ctx, uuid, desired, groupsFromResponse(current)); err != nil {
			resp.Diagnostics.AddError("Error setting user groups", err.Error())
			return
		}
	}

	// 4. readback
	got, err := r.getUser(ctx, uuid)
	if err != nil {
		resp.Diagnostics.AddError("Error reading user after update", err.Error())
		return
	}
	pref := desired
	if !managed {
		pref, _ = stringList(state.Groups)
	}
	ver := state.PasswordWoVersion
	applyUserResponse(&state, got, pref)
	state.PasswordWoVersion = ver
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *authUserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data resource_auth_user.AuthUserCustomModel
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
		resp.Diagnostics.AddError("Refusing to delete user", err.Error())
		return
	}
	if err := r.call(ctx, rest.HTTP_DELETE, delete_rs_authUser, uuid, nil, nil); err != nil && !apiclient.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting user", err.Error())
	}
}

// Configure adds the provider configured client to the resource.
func (r *authUserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*apiclient.EdaApiClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *api.EdaApiClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	r.client = client
}

// ImportState implements resource.ResourceWithImportState. The import ID is the user UUID.
func (r *authUserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := strings.TrimSpace(req.ID)
	if !resource_auth_user.IsUUID(id) {
		resp.Diagnostics.AddError("Invalid ID", "Expected format: id = <user uuid>")
		return
	}
	if r.client == nil {
		resp.Diagnostics.AddError("Provider not configured", "the API client is not configured")
		return
	}
	if _, err := r.guardActual(ctx, id); err != nil {
		resp.Diagnostics.AddError("Cannot import user", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("uuid"), id)...)
}
