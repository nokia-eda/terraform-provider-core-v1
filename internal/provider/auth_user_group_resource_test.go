package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-log/tflogtest"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/eda/apiclient"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/resource_auth_user_group"
)

// fakeGroupAPI mimics the observed EDA behaviour: POST rejects roles, PUT on
// the group ignores roles and users, PUT .../roles replaces the roles.
type fakeGroupAPI struct {
	groups map[string]map[string]any
	calls  []call
	n      int
	fail   map[string]error
}

func newFakeGroups() *fakeGroupAPI {
	return &fakeGroupAPI{groups: map[string]map[string]any{}, fail: map[string]error{}}
}

func (f *fakeGroupAPI) paths() []string {
	var out []string
	for _, c := range f.calls {
		out = append(out, c.Method+" "+c.Path)
	}
	return out
}

func (f *fakeGroupAPI) view(g map[string]any) map[string]any {
	out := map[string]any{"uuid": g["uuid"], "name": g["name"], "description": g["description"]}
	if r, _ := g["roles"].([]string); len(r) > 0 {
		arr := []any{}
		for _, x := range r {
			arr = append(arr, x)
		}
		out["roles"] = arr
	}
	if u, _ := g["users"].([]string); len(u) > 0 {
		arr := []any{}
		for _, x := range u {
			arr = append(arr, x)
		}
		out["users"] = arr
	}
	return out
}

func (f *fakeGroupAPI) Execute(ctx context.Context, pathUrl, method string, pp, qp map[string]string, body, result any) error {
	b, _ := json.Marshal(body)
	uuid := pp["uuid"]
	f.calls = append(f.calls, call{method, pathUrl, uuid, string(b)})
	if err, ok := f.fail[method+" "+pathUrl]; ok {
		return err
	}
	set := func(m map[string]any) {
		if r, ok := result.(*map[string]any); ok && r != nil {
			j, _ := json.Marshal(m)
			*r = map[string]any{}
			_ = json.Unmarshal(j, r)
		}
	}
	g, exists := f.groups[uuid]
	switch {
	case method == "POST" && pathUrl == create_rs_authUserGroup:
		m := body.(map[string]any)
		if _, has := m["roles"]; has {
			return &apiclient.APIError{StatusCode: 400, Status: "400", Body: "CannotSpecifyRolesOnCreate"}
		}
		f.n++
		id := fmt.Sprintf("00000000-0000-0000-0000-%012d", f.n)
		f.groups[id] = map[string]any{"uuid": id, "name": m["name"], "description": m["description"]}
		set(f.view(f.groups[id]))
	case method == "GET" && pathUrl == read_rs_authUserGroup:
		if !exists {
			return notFound()
		}
		set(f.view(g))
	case method == "PUT" && pathUrl == update_rs_authUserGroup:
		if !exists {
			return notFound()
		}
		m := body.(map[string]any)
		g["name"], g["description"] = m["name"], m["description"] // roles/users ignored, as EDA does
		set(f.view(g))
	case method == "PUT" && pathUrl == roles_rs_authUserGroup:
		if !exists {
			return notFound()
		}
		for _, r := range body.([]string) {
			if r == "eda:missing" {
				return &apiclient.APIError{StatusCode: 404, Status: "404", Body: "RoleNotFound"}
			}
		}
		g["roles"] = append([]string(nil), body.([]string)...)
	case method == "DELETE" && pathUrl == delete_rs_authUserGroup:
		if !exists {
			return notFound()
		}
		delete(f.groups, uuid)
	default:
		return fmt.Errorf("unexpected %s %s", method, pathUrl)
	}
	return nil
}

func gSchema(t *testing.T) (resource.SchemaResponse, tftypes.Type) {
	var sr resource.SchemaResponse
	(&authUserGroupResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sr)
	return sr, sr.Schema.Type().TerraformType(context.Background())
}

func gRaw(t *testing.T, m resource_auth_user_group.GroupModel) tftypes.Value {
	sr, typ := gSchema(t)
	st := tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(typ, nil)}
	if d := st.Set(context.Background(), &m); d.HasError() {
		t.Fatalf("set: %v", d)
	}
	return st.Raw
}

func gPlan(name, desc string, roles []string, uuid string) resource_auth_user_group.GroupModel {
	m := resource_auth_user_group.GroupModel{
		Name: types.StringValue(name), Description: types.StringValue(desc), Roles: setOf(roles),
		Users: types.SetUnknown(types.StringType), Uuid: types.StringUnknown(),
	}
	if uuid != "" {
		m.Uuid = types.StringValue(uuid)
	}
	return m
}

func gCreate(t *testing.T, f *fakeGroupAPI, plan resource_auth_user_group.GroupModel) (*resource.CreateResponse, resource_auth_user_group.GroupModel) {
	sr, typ := gSchema(t)
	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: sr.Schema, Raw: gRaw(t, plan)}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(typ, nil)}}
	(&authUserGroupResource{client: f}).Create(context.Background(), req, resp)
	var m resource_auth_user_group.GroupModel
	resp.State.Get(context.Background(), &m)
	return resp, m
}

func gUpdate(t *testing.T, f *fakeGroupAPI, state, plan resource_auth_user_group.GroupModel) (*resource.UpdateResponse, resource_auth_user_group.GroupModel) {
	sr, _ := gSchema(t)
	req := resource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: sr.Schema, Raw: gRaw(t, plan)},
		State: tfsdk.State{Schema: sr.Schema, Raw: gRaw(t, state)},
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: gRaw(t, state)}}
	(&authUserGroupResource{client: f}).Update(context.Background(), req, resp)
	var m resource_auth_user_group.GroupModel
	resp.State.Get(context.Background(), &m)
	return resp, m
}

func rolesOf(m resource_auth_user_group.GroupModel) []string {
	r, _ := setStrings(m.Roles)
	return r
}

func TestGroupCreateWithRolesUsesRolesEndpoint(t *testing.T) {
	f := newFakeGroups()
	resp, m := gCreate(t, f, gPlan("tftest-g", "d", []string{"eda:network-operator", "view"}, ""))
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if got := strings.Join(f.paths(), ","); got != "POST /core/admin/groups,PUT /core/admin/groups/{uuid}/roles,GET /core/admin/groups/{uuid}" {
		t.Fatalf("call order: %s", got)
	}
	if strings.Contains(f.calls[0].Body, "roles") {
		t.Fatalf("POST body must not carry roles: %s", f.calls[0].Body)
	}
	if f.calls[1].Body != `["eda:network-operator","view"]` {
		t.Fatalf("roles body: %s", f.calls[1].Body)
	}
	if strings.Join(rolesOf(m), ",") != "eda:network-operator,view" || m.Uuid.ValueString() == "" {
		t.Fatalf("state: %+v", m)
	}
}

func TestGroupCreateWithoutRolesSkipsRolesCall(t *testing.T) {
	f := newFakeGroups()
	resp, m := gCreate(t, f, gPlan("tftest-g", "", nil, ""))
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	for _, p := range f.paths() {
		if strings.Contains(p, "/roles") {
			t.Fatalf("unexpected roles call: %v", f.paths())
		}
	}
	if len(rolesOf(m)) != 0 || m.Roles.IsNull() {
		t.Fatalf("roles should be an empty set: %+v", m.Roles)
	}
}

func TestGroupCreateRolesFailureKeepsPartialState(t *testing.T) {
	f := newFakeGroups()
	resp, m := gCreate(t, f, gPlan("tftest-g", "", []string{"eda:missing"}, ""))
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected error")
	}
	if m.Uuid.ValueString() == "" {
		t.Fatal("partial state must carry the uuid so the group is tainted and cleaned up")
	}
}

func TestGroupUpdateBodyIsWhitelistAndRolesReplaced(t *testing.T) {
	f := newFakeGroups()
	_, st := gCreate(t, f, gPlan("tftest-g", "d", []string{"view"}, ""))
	f.groups[st.Uuid.ValueString()]["users"] = []string{"alice"} // membership owned elsewhere
	f.calls = nil
	plan := gPlan("tftest-g", "new", []string{"eda:network-operator"}, st.Uuid.ValueString())
	plan.Users = st.Users
	resp, m := gUpdate(t, f, st, plan)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	put := f.calls[1]
	if put.Method != "PUT" || put.Path != update_rs_authUserGroup || put.Body != `{"description":"new","name":"tftest-g"}` {
		t.Fatalf("group PUT body must be {name, description} only: %+v", put)
	}
	if strings.Join(rolesOf(m), ",") != "eda:network-operator" {
		t.Fatalf("roles not replaced: %v", rolesOf(m))
	}
	if u, _ := setStrings(m.Users); strings.Join(u, ",") != "alice" {
		t.Fatalf("users must be read back untouched: %v", u)
	}
	for _, c := range f.calls {
		if strings.Contains(c.Body, "alice") {
			t.Fatalf("users must never be sent: %+v", c)
		}
	}
}

func TestGroupUpdateClearsRoles(t *testing.T) {
	f := newFakeGroups()
	_, st := gCreate(t, f, gPlan("tftest-g", "", []string{"view"}, ""))
	f.calls = nil
	plan := gPlan("tftest-g", "", nil, st.Uuid.ValueString())
	plan.Users = st.Users
	resp, m := gUpdate(t, f, st, plan)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if c := f.find("PUT", roles_rs_authUserGroup); c == nil || c.Body != `[]` {
		t.Fatalf("clearing must PUT []: %+v", f.calls)
	}
	if len(rolesOf(m)) != 0 {
		t.Fatalf("roles not cleared: %v", rolesOf(m))
	}
}

func TestGroupUpdateNoChangeMakesNoWrite(t *testing.T) {
	f := newFakeGroups()
	_, st := gCreate(t, f, gPlan("tftest-g", "d", []string{"view"}, ""))
	f.calls = nil
	plan := gPlan("tftest-g", "d", []string{"view"}, st.Uuid.ValueString())
	plan.Users = st.Users
	if resp, _ := gUpdate(t, f, st, plan); resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	for _, c := range f.calls {
		if c.Method != "GET" {
			t.Fatalf("no write expected: %v", f.paths())
		}
	}
}

func TestGroupReadRemovesMissingAndRefusesBuiltIn(t *testing.T) {
	f := newFakeGroups()
	sr, _ := gSchema(t)
	res := &authUserGroupResource{client: f}
	state := gPlan("gone", "", nil, "00000000-0000-0000-0000-00000000dead")
	state.Users = setOf(nil)
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: sr.Schema, Raw: gRaw(t, state)}}
	res.Read(context.Background(), resource.ReadRequest{State: resp.State}, resp)
	if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
		t.Fatalf("404 must remove the resource: %v", resp.Diagnostics)
	}
	f.groups["b"] = map[string]any{"uuid": "b", "name": "system-administrator", "description": ""}
	state = gPlan("x", "", nil, "b")
	state.Users = setOf(nil)
	resp = &resource.ReadResponse{State: tfsdk.State{Schema: sr.Schema, Raw: gRaw(t, state)}}
	res.Read(context.Background(), resource.ReadRequest{State: resp.State}, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("built-in group must be refused")
	}
}

func TestGroupDeleteGuardsAndIgnores404(t *testing.T) {
	f := newFakeGroups()
	sr, _ := gSchema(t)
	res := &authUserGroupResource{client: f}
	f.groups["b"] = map[string]any{"uuid": "b", "name": "system-administrator"}
	state := gPlan("x", "", nil, "b")
	state.Users = setOf(nil)
	resp := &resource.DeleteResponse{}
	res.Delete(context.Background(), resource.DeleteRequest{State: tfsdk.State{Schema: sr.Schema, Raw: gRaw(t, state)}}, resp)
	if !resp.Diagnostics.HasError() || f.groups["b"] == nil {
		t.Fatal("built-in group must not be deleted")
	}
	state = gPlan("x", "", nil, "00000000-0000-0000-0000-00000000dead")
	state.Users = setOf(nil)
	resp = &resource.DeleteResponse{}
	res.Delete(context.Background(), resource.DeleteRequest{State: tfsdk.State{Schema: sr.Schema, Raw: gRaw(t, state)}}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("404 on delete must succeed: %v", resp.Diagnostics)
	}
}

func TestGroupValidateConfig(t *testing.T) {
	if e := resource_auth_user_group.ValidateGroupModel(&resource_auth_user_group.GroupModel{
		Name: types.StringValue("System-Administrator"), Roles: setOf(nil)}); len(e) != 1 {
		t.Fatalf("protected name: %v", e)
	}
	if e := resource_auth_user_group.ValidateGroupModel(&resource_auth_user_group.GroupModel{
		Name: types.StringValue("ok"), Roles: setOf([]string{"a:b:c", " "})}); len(e) != 2 {
		t.Fatalf("roles: %v", e)
	}
	if e := resource_auth_user_group.ValidateGroupModel(&resource_auth_user_group.GroupModel{
		Name: types.StringValue("ok"), Roles: setOf([]string{"eda:network-operator", "view"})}); len(e) != 0 {
		t.Fatalf("valid: %v", e)
	}
}

func TestGroupSchemaDropsHeavyAttributes(t *testing.T) {
	sr, _ := gSchema(t)
	keys := []string{}
	for k := range sr.Schema.Attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != "description,name,roles,users,uuid" {
		t.Fatalf("attributes: %v", keys)
	}
}

// Wire test: real apiclient/resty path, request encoding and no body logging.
func TestWireGroupRolesBindAndNoBodyLogging(t *testing.T) {
	var logs bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &logs)
	var seen []string
	var rolesBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/core/httpproxy/v1/keycloak/realms/eda/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"wire-access-token","refresh_token":"x","expires_in":300}`))
	})
	const id = "22222222-2222-2222-2222-222222222222"
	mux.HandleFunc("/core/admin/groups", func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"uuid":"` + id + `","name":"tftest-w","description":"secret-looking-description"}`))
	})
	mux.HandleFunc("/core/admin/groups/"+id+"/roles", func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		b, _ := io.ReadAll(r.Body)
		rolesBody = string(b)
		w.WriteHeader(204)
	})
	mux.HandleFunc("/core/admin/groups/"+id, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"uuid":"` + id + `","name":"tftest-w","description":"secret-looking-description","roles":["eda:network-operator"]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := wireClient(t, ctx, srv.URL, false)

	sr, typ := gSchema(t)
	req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: sr.Schema, Raw: gRaw(t, gPlan("tftest-w", "secret-looking-description", []string{"eda:network-operator"}, ""))}}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(typ, nil)}}
	(&authUserGroupResource{client: c}).Create(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if strings.Join(seen, ",") != "POST /core/admin/groups,PUT /core/admin/groups/"+id+"/roles,GET /core/admin/groups/"+id {
		t.Fatalf("wire calls: %v", seen)
	}
	if rolesBody != `["eda:network-operator"]` {
		t.Fatalf("roles wire body: %s", rolesBody)
	}
	if strings.Contains(logs.String(), "secret-looking-description") {
		t.Fatal("request/response bodies must not be logged")
	}
}

func (f *fakeGroupAPI) find(m, p string) *call {
	for i := range f.calls {
		if f.calls[i].Method == m && f.calls[i].Path == p {
			return &f.calls[i]
		}
	}
	return nil
}
