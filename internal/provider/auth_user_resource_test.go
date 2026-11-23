package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/eda/apiclient"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/resource_auth_user"
)

const testSecret = "S3cr3t-Pa55-ZZZ"

type call struct {
	Method, Path, UUID string
	Body               string
}

type fakeAPI struct {
	users    map[string]map[string]any
	calls    []call
	fail     map[string]error // "METHOD path" -> error
	passwds  map[string]string
	n        int
	noEnable bool // simulate server forcing enabled=false on create
	count    map[string]int
	failFrom map[string]int // "METHOD path" -> fail from the Nth call (1-based)
}

func newFake() *fakeAPI {
	return &fakeAPI{users: map[string]map[string]any{}, fail: map[string]error{}, failFrom: map[string]int{}, passwds: map[string]string{}}
}

func notFound() error {
	return &apiclient.APIError{StatusCode: 404, Status: "404 Not Found", Body: "nope"}
}

func (f *fakeAPI) methods() []string {
	var out []string
	for _, c := range f.calls {
		out = append(out, c.Method+" "+c.Path)
	}
	return out
}

func (f *fakeAPI) has(m, p string) bool {
	for _, c := range f.calls {
		if c.Method == m && c.Path == p {
			return true
		}
	}
	return false
}

func (f *fakeAPI) find(m, p string) *call {
	for i := range f.calls {
		if f.calls[i].Method == m && f.calls[i].Path == p {
			return &f.calls[i]
		}
	}
	return nil
}

func (f *fakeAPI) view(u map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range u {
		out[k] = v
	}
	if g, _ := u["groups"].([]string); len(g) > 0 {
		s := append([]string(nil), g...)
		sort.Strings(s) // server reorders
		arr := make([]any, len(s))
		for i := range s {
			arr[i] = s[i]
		}
		out["groups"] = arr
	} else {
		delete(out, "groups")
	}
	out["status"] = map[string]any{}
	return out
}

func (f *fakeAPI) Execute(ctx context.Context, pathUrl, method string, pp, qp map[string]string, body, result any) error {
	b, _ := json.Marshal(body)
	uuid := pp["uuid"]
	f.calls = append(f.calls, call{method, pathUrl, uuid, string(b)})
	if err, ok := f.fail[method+" "+pathUrl]; ok {
		return err
	}
	if f.count == nil {
		f.count = map[string]int{}
	}
	f.count[method+" "+pathUrl]++
	if n, ok := f.failFrom[method+" "+pathUrl]; ok && f.count[method+" "+pathUrl] >= n {
		return fmt.Errorf("injected failure")
	}
	set := func(m map[string]any) {
		if r, ok := result.(*map[string]any); ok && r != nil {
			j, _ := json.Marshal(m)
			*r = map[string]any{}
			_ = json.Unmarshal(j, r)
		}
	}
	u, exists := f.users[uuid]
	switch {
	case method == "POST" && pathUrl == "/core/admin/users":
		f.n++
		id := fmt.Sprintf("00000000-0000-0000-0000-%012d", f.n)
		m := body.(map[string]any)
		nu := map[string]any{"uuid": id}
		for k, v := range m {
			nu[k] = v
		}
		if f.noEnable {
			nu["enabled"] = false
		}
		f.users[id] = nu
		set(f.view(nu))
	case method == "GET" && pathUrl == read_rs_authUser:
		if !exists {
			return notFound()
		}
		set(f.view(u))
	case method == "PUT" && pathUrl == update_rs_authUser:
		if !exists {
			return notFound()
		}
		for k, v := range body.(map[string]any) {
			u[k] = v
		}
		set(f.view(u))
	case method == "PUT" && pathUrl == resetpassword_rs_authUser:
		if !exists {
			return notFound()
		}
		f.passwds[uuid] = body.(map[string]any)["value"].(string)
		u["enabled"] = true
	case method == "PUT" && pathUrl == groups_rs_authUser:
		g := body.([]string)
		if len(g) == 0 {
			return &apiclient.APIError{StatusCode: 400, Status: "400", Body: "NoGroupIDsProvided"}
		}
		u["groups"] = append([]string(nil), g...)
	case method == "DELETE" && pathUrl == groups_rs_authUser:
		u["groups"] = []string(nil)
	case method == "DELETE" && pathUrl == delete_rs_authUser:
		if !exists {
			return notFound()
		}
		delete(f.users, uuid)
	default:
		return fmt.Errorf("unexpected %s %s", method, pathUrl)
	}
	return nil
}

// ---------------------------------------------------------------------------

func newRes(f authUserAPI) *authUserResource { return &authUserResource{client: f} }

func schemaOf(t *testing.T) (resource.SchemaResponse, tftypes.Type) {
	var sr resource.SchemaResponse
	(&authUserResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sr)
	return sr, sr.Schema.Type().TerraformType(context.Background())
}

type spec struct {
	username, email, first, last string
	enabled                      *bool
	groups                       []string // nil => unknown (unmanaged) in plan, null in config
	groupsSet                    bool
	version                      *int64
	pw                           string
	uuid                         string
	stateGroups                  []string
}

func (s spec) model(plan bool) resource_auth_user.AuthUserCustomModel {
	m := resource_auth_user.AuthUserCustomModel{
		Username: types.StringValue(s.username), Email: types.StringValue(s.email),
		FirstName: types.StringValue(s.first), LastName: types.StringValue(s.last),
		Enabled:           types.BoolValue(true),
		MaxSessions:       types.Int64Unknown(),
		Groups:            types.ListNull(types.StringType),
		Status:            types.ObjectUnknown(resource_auth_user.StatusAttrTypes),
		Uuid:              types.StringUnknown(),
		Password:          types.StringNull(),
		PasswordWo:        types.StringNull(),
		PasswordWoVersion: types.Int64Null(),
	}
	if s.enabled != nil {
		m.Enabled = types.BoolValue(*s.enabled)
	}
	if s.groupsSet {
		m.Groups = listOf(s.groups)
	} else if plan {
		m.Groups = types.ListUnknown(types.StringType)
	}
	if s.version != nil {
		m.PasswordWoVersion = types.Int64Value(*s.version)
	}
	if s.uuid != "" {
		m.Uuid = types.StringValue(s.uuid)
	}
	if !plan {
		m.MaxSessions = types.Int64Null()
		m.Status = types.ObjectNull(resource_auth_user.StatusAttrTypes)
		m.Uuid = types.StringNull()
	}
	return m
}

func rawOf(t *testing.T, m resource_auth_user.AuthUserCustomModel) tftypes.Value {
	sr, typ := schemaOf(t)
	st := tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(typ, nil)}
	if d := st.Set(context.Background(), &m); d.HasError() {
		t.Fatalf("set: %v", d)
	}
	return st.Raw
}

func i64(n int64) *int64 { return &n }

func boolp(b bool) *bool { return &b }

func create(t *testing.T, f *fakeAPI, s spec) (*resource.CreateResponse, tfsdk.State) {
	sr, typ := schemaOf(t)
	plan := s.model(true)
	cfg := s.model(false)
	if s.pw != "" {
		cfg.PasswordWo = types.StringValue(s.pw)
	}
	req := resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: sr.Schema, Raw: rawOf(t, plan)},
		Config: tfsdk.Config{Schema: sr.Schema, Raw: rawOf(t, cfg)},
	}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(typ, nil)}}
	newRes(f).Create(context.Background(), req, resp)
	return resp, resp.State
}

func getModel(t *testing.T, st tfsdk.State) resource_auth_user.AuthUserCustomModel {
	var m resource_auth_user.AuthUserCustomModel
	if d := st.Get(context.Background(), &m); d.HasError() {
		t.Fatalf("get: %v", d)
	}
	return m
}

func assertNoSecret(t *testing.T, f *fakeAPI, st tfsdk.State) {
	if strings.Contains(st.Raw.String(), testSecret) {
		t.Fatalf("secret in state")
	}
	m := getModel(t, st)
	if !m.PasswordWo.IsNull() || !m.Password.IsNull() {
		t.Fatalf("password attrs not null in state")
	}
}

func baseSpec() spec {
	return spec{username: "tftest-a", email: "a@example.invalid", first: "A", last: "B"}
}

func groupList(t *testing.T, m resource_auth_user.AuthUserCustomModel) []string {
	g, ok := stringList(m.Groups)
	if !ok {
		t.Fatalf("groups null/unknown")
	}
	return g
}

func TestCreateOrderAndBodies(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.pw, s.version, s.groups, s.groupsSet = testSecret, i64(1), []string{"g2", "g1"}, true
	resp, st := create(t, f, s)
	if resp.Diagnostics.HasError() {
		t.Fatalf("%v", resp.Diagnostics)
	}
	want := []string{"POST /core/admin/users", "PUT " + resetpassword_rs_authUser, "GET " + read_rs_authUser, "PUT " + groups_rs_authUser, "GET " + read_rs_authUser}
	if got := f.methods(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order %v want %v", got, want)
	}
	cb := f.find("POST", create_rs_authUser).Body
	var body map[string]any
	_ = json.Unmarshal([]byte(cb), &body)
	keys := []string{}
	for k := range body {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != "email,enabled,firstName,lastName,username" {
		t.Fatalf("identity body keys: %v", keys)
	}
	if strings.Contains(cb, testSecret) || strings.Contains(cb, "password") || strings.Contains(cb, "groups") {
		t.Fatalf("identity body leaks: %s", cb)
	}
	if got := f.find("PUT", resetpassword_rs_authUser).Body; got != `{"temporary":false,"value":"`+testSecret+`"}` {
		t.Fatalf("reset body %s", got)
	}
	if got := f.find("PUT", groups_rs_authUser).Body; got != `["g2","g1"]` {
		t.Fatalf("groups body %s", got)
	}
	m := getModel(t, st)
	assertNoSecret(t, f, st)
	if m.PasswordWoVersion.ValueInt64() != 1 || m.Uuid.ValueString() == "" || !m.Enabled.ValueBool() {
		t.Fatalf("bad state %+v", m)
	}
	// server returns sorted [g1 g2]; state must keep configured order
	if g := groupList(t, m); strings.Join(g, ",") != "g2,g1" {
		t.Fatalf("groups order %v", g)
	}
}

func TestCreateNoGroupsManagedNoPassword(t *testing.T) {
	f := newFake()
	resp, st := create(t, f, baseSpec()) // groups unmanaged
	if resp.Diagnostics.HasError() {
		t.Fatalf("%v", resp.Diagnostics)
	}
	if f.has("PUT", groups_rs_authUser) || f.has("PUT", resetpassword_rs_authUser) {
		t.Fatalf("unexpected calls %v", f.methods())
	}
	m := getModel(t, st)
	if m.Groups.IsNull() || len(m.Groups.Elements()) != 0 {
		t.Fatalf("groups must be [] not null: %v", m.Groups)
	}
	if !m.PasswordWoVersion.IsNull() {
		t.Fatalf("version must be null")
	}
}

func TestCreateEmptyGroupsNoCall(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.groups, s.groupsSet = []string{}, true
	resp, st := create(t, f, s)
	if resp.Diagnostics.HasError() {
		t.Fatalf("%v", resp.Diagnostics)
	}
	if f.has("PUT", groups_rs_authUser) || f.has("DELETE", groups_rs_authUser) {
		t.Fatalf("no group call expected: %v", f.methods())
	}
	if m := getModel(t, st); m.Groups.IsNull() {
		t.Fatalf("null groups")
	}
}

func TestCreatePasswordWithoutVersionRejected(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.pw = testSecret
	resp, _ := create(t, f, s)
	if !resp.Diagnostics.HasError() || len(f.calls) != 0 {
		t.Fatalf("expected error without calls: %v %v", resp.Diagnostics, f.methods())
	}
}

func TestCreateResetPasswordFailurePersistsUUID(t *testing.T) {
	f := newFake()
	f.fail["PUT "+resetpassword_rs_authUser] = fmt.Errorf("boom")
	s := baseSpec()
	s.pw, s.version = testSecret, i64(3)
	resp, st := create(t, f, s)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected error")
	}
	m := getModel(t, st)
	if m.Uuid.ValueString() == "" {
		t.Fatalf("uuid must be persisted in partial state")
	}
	if !m.PasswordWoVersion.IsNull() {
		t.Fatalf("version must stay unset when password failed")
	}
	if f.has("PUT", groups_rs_authUser) {
		t.Fatalf("groups must not be attempted")
	}
	assertNoSecret(t, f, st)
	if strings.Contains(fmt.Sprint(resp.Diagnostics), testSecret) {
		t.Fatalf("diagnostics leak secret")
	}
}

func TestCreateGroupFailureKeepsVersionAndUUID(t *testing.T) {
	f := newFake()
	f.fail["PUT "+groups_rs_authUser] = fmt.Errorf("boom")
	s := baseSpec()
	s.pw, s.version, s.groups, s.groupsSet = testSecret, i64(2), []string{"g1"}, true
	resp, st := create(t, f, s)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected error")
	}
	m := getModel(t, st)
	if m.Uuid.ValueString() == "" || m.PasswordWoVersion.ValueInt64() != 2 {
		t.Fatalf("partial state wrong: %+v", m)
	}
}

func TestCreateEnabledConvergence(t *testing.T) {
	f := newFake()
	f.noEnable = true
	resp, st := create(t, f, baseSpec())
	if resp.Diagnostics.HasError() {
		t.Fatalf("%v", resp.Diagnostics)
	}
	_ = st
	// server forced enabled=false on create; a corrective PUT must follow
	puts := 0
	for _, c := range f.calls {
		if c.Method == "PUT" && c.Path == update_rs_authUser {
			puts++
			if !strings.Contains(c.Body, `"enabled":true`) {
				t.Fatalf("corrective body %s", c.Body)
			}
		}
	}
	if puts != 1 {
		t.Fatalf("want 1 corrective PUT, got %d", puts)
	}
	if !getModel(t, st).Enabled.ValueBool() {
		t.Fatalf("enabled not true in state")
	}
}

func TestCreateMissingUUIDInResponse(t *testing.T) {
	f := newFake()
	r := newRes(&noUUIDAPI{})
	sr, typ := schemaOf(t)
	s := baseSpec()
	req := resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: sr.Schema, Raw: rawOf(t, s.model(true))},
		Config: tfsdk.Config{Schema: sr.Schema, Raw: rawOf(t, s.model(false))},
	}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(typ, nil)}}
	r.Create(context.Background(), req, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected error")
	}
	_ = f
}

type noUUIDAPI struct{}

func (noUUIDAPI) Execute(ctx context.Context, p, m string, pp, qp map[string]string, body, result any) error {
	return nil
}

// ---------------------------------------------------------------------------
// Update / Read / Delete
// ---------------------------------------------------------------------------

func seed(t *testing.T, f *fakeAPI, s spec) tfsdk.State {
	f.calls = nil
	resp, st := create(t, f, s)
	if resp.Diagnostics.HasError() {
		t.Fatalf("seed: %v", resp.Diagnostics)
	}
	f.calls = nil
	return st
}

func update(t *testing.T, f *fakeAPI, prior tfsdk.State, s spec) (*resource.UpdateResponse, tfsdk.State) {
	sr, _ := schemaOf(t)
	pm := getModel(t, prior)
	plan := s.model(true)
	plan.Uuid = pm.Uuid
	plan.MaxSessions = pm.MaxSessions
	plan.Status = types.ObjectUnknown(resource_auth_user.StatusAttrTypes)
	if !s.groupsSet {
		plan.Groups = pm.Groups
	}
	cfg := s.model(false)
	if s.pw != "" {
		cfg.PasswordWo = types.StringValue(s.pw)
	}
	req := resource.UpdateRequest{
		Plan:   tfsdk.Plan{Schema: sr.Schema, Raw: rawOf(t, plan)},
		State:  prior,
		Config: tfsdk.Config{Schema: sr.Schema, Raw: rawOf(t, cfg)},
	}
	resp := &resource.UpdateResponse{State: prior}
	newRes(f).Update(context.Background(), req, resp)
	return resp, resp.State
}

func TestUpdateNoVersionChangeNoReset(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.pw, s.version, s.groups, s.groupsSet = testSecret, i64(1), []string{"g1"}, true
	st := seed(t, f, s)
	s.first = "Changed"
	resp, st2 := update(t, f, st, s)
	if resp.Diagnostics.HasError() {
		t.Fatalf("%v", resp.Diagnostics)
	}
	if f.has("PUT", resetpassword_rs_authUser) {
		t.Fatalf("must not reset password: %v", f.methods())
	}
	ub := f.find("PUT", update_rs_authUser).Body
	if strings.Contains(ub, "password") || strings.Contains(ub, "groups") || strings.Contains(ub, testSecret) {
		t.Fatalf("update body leaks: %s", ub)
	}
	if f.has("PUT", groups_rs_authUser) {
		t.Fatalf("groups unchanged -> no call: %v", f.methods())
	}
	m := getModel(t, st2)
	if m.FirstName.ValueString() != "Changed" || m.PasswordWoVersion.ValueInt64() != 1 {
		t.Fatalf("bad %+v", m)
	}
	assertNoSecret(t, f, st2)
}

func TestUpdateVersionBumpResets(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.pw, s.version = testSecret, i64(1)
	st := seed(t, f, s)
	s.pw, s.version = "new-"+testSecret, i64(2)
	resp, st2 := update(t, f, st, s)
	if resp.Diagnostics.HasError() {
		t.Fatalf("%v", resp.Diagnostics)
	}
	if f.has("PUT", update_rs_authUser) {
		t.Fatalf("identity unchanged -> no PUT identity")
	}
	if f.passwds[getModel(t, st2).Uuid.ValueString()] != "new-"+testSecret {
		t.Fatalf("password not reset")
	}
	if getModel(t, st2).PasswordWoVersion.ValueInt64() != 2 {
		t.Fatalf("version not stored")
	}
	if strings.Contains(st2.Raw.String(), testSecret) {
		t.Fatalf("secret in state")
	}
}

func TestUpdateVersionBumpFailureKeepsOldVersion(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.pw, s.version = testSecret, i64(1)
	st := seed(t, f, s)
	f.fail["PUT "+resetpassword_rs_authUser] = fmt.Errorf("boom")
	s.pw, s.version = "x"+testSecret, i64(2)
	resp, st2 := update(t, f, st, s)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected error")
	}
	if getModel(t, st2).PasswordWoVersion.ValueInt64() != 1 {
		t.Fatalf("version must stay 1 so the rotation is retried")
	}
}

func TestUpdateVersionBumpWithoutPasswordRejected(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.pw, s.version = testSecret, i64(1)
	st := seed(t, f, s)
	s.pw, s.version = "", i64(2)
	resp, _ := update(t, f, st, s)
	if !resp.Diagnostics.HasError() || len(f.calls) != 0 {
		t.Fatalf("expected rejection")
	}
}

func TestUpdateGroupsReorderNoCallPreservesOrder(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.groups, s.groupsSet = []string{"g1", "g2"}, true
	st := seed(t, f, s)
	s.groups = []string{"g2", "g1"}
	resp, st2 := update(t, f, st, s)
	if resp.Diagnostics.HasError() {
		t.Fatalf("%v", resp.Diagnostics)
	}
	if f.has("PUT", groups_rs_authUser) || f.has("DELETE", groups_rs_authUser) {
		t.Fatalf("reorder must not call API")
	}
	if g := groupList(t, getModel(t, st2)); strings.Join(g, ",") != "g2,g1" {
		t.Fatalf("order %v", g)
	}
}

func TestUpdateGroupsClearAll(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.groups, s.groupsSet = []string{"g1", "g2"}, true
	st := seed(t, f, s)
	s.groups = []string{}
	resp, st2 := update(t, f, st, s)
	if resp.Diagnostics.HasError() {
		t.Fatalf("%v", resp.Diagnostics)
	}
	c := f.find("DELETE", groups_rs_authUser)
	if c == nil || c.Body != `["g1","g2"]` || f.has("PUT", groups_rs_authUser) {
		t.Fatalf("clear must DELETE current groups: %v %+v", f.methods(), c)
	}
	m := getModel(t, st2)
	if m.Groups.IsNull() || len(m.Groups.Elements()) != 0 {
		t.Fatalf("groups must be [] after clear")
	}
}

func TestUpdateGroupsReplace(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.groups, s.groupsSet = []string{"g1", "g2"}, true
	st := seed(t, f, s)
	s.groups = []string{"g3"}
	resp, st2 := update(t, f, st, s)
	if resp.Diagnostics.HasError() {
		t.Fatalf("%v", resp.Diagnostics)
	}
	if c := f.find("PUT", groups_rs_authUser); c == nil || c.Body != `["g3"]` {
		t.Fatalf("expected PUT [g3]: %v", f.methods())
	}
	if g := groupList(t, getModel(t, st2)); strings.Join(g, ",") != "g3" {
		t.Fatalf("%v", g)
	}
}

func readState(t *testing.T, f *fakeAPI, st tfsdk.State) (*resource.ReadResponse, tfsdk.State) {
	resp := &resource.ReadResponse{State: st}
	newRes(f).Read(context.Background(), resource.ReadRequest{State: st}, resp)
	return resp, resp.State
}

func TestRead404RemovesState(t *testing.T) {
	f := newFake()
	st := seed(t, f, baseSpec())
	delete(f.users, getModel(t, st).Uuid.ValueString())
	resp, st2 := readState(t, f, st)
	if resp.Diagnostics.HasError() {
		t.Fatalf("%v", resp.Diagnostics)
	}
	if !st2.Raw.IsNull() {
		t.Fatalf("state must be removed on 404")
	}
}

func TestReadOtherErrorSurfaces(t *testing.T) {
	f := newFake()
	st := seed(t, f, baseSpec())
	f.fail["GET "+read_rs_authUser] = &apiclient.APIError{StatusCode: 500, Status: "500", Body: "x"}
	resp, _ := readState(t, f, st)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected error")
	}
}

func TestReadNormalisesAbsentGroupsAndKeepsOrder(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.groups, s.groupsSet = []string{"g2", "g1"}, true
	st := seed(t, f, s)
	_, st2 := readState(t, f, st)
	if g := groupList(t, getModel(t, st2)); strings.Join(g, ",") != "g2,g1" {
		t.Fatalf("order not preserved %v", g)
	}
	// out of band membership removal -> [] not null
	f.users[getModel(t, st).Uuid.ValueString()]["groups"] = []string(nil)
	_, st3 := readState(t, f, st2)
	m := getModel(t, st3)
	if m.Groups.IsNull() || len(m.Groups.Elements()) != 0 {
		t.Fatalf("groups must be [] when absent")
	}
}

func TestReadAfterImportHasNoVersion(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.pw, s.version = testSecret, i64(4)
	st := seed(t, f, s)
	id := getModel(t, st).Uuid.ValueString()
	sr, typ := schemaOf(t)
	imp := &resource.ImportStateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(typ, nil)}}
	newRes(f).ImportState(context.Background(), resource.ImportStateRequest{ID: id}, imp)
	if imp.Diagnostics.HasError() {
		t.Fatalf("%v", imp.Diagnostics)
	}
	_, st2 := readState(t, f, imp.State)
	m := getModel(t, st2)
	if !m.PasswordWoVersion.IsNull() || m.Username.ValueString() != "tftest-a" {
		t.Fatalf("import state wrong: %+v", m)
	}
}

func TestImportRejectsInvalidID(t *testing.T) {
	sr, typ := schemaOf(t)
	for _, id := range []string{"", "   ", "not-a-uuid", "a/b"} {
		imp := &resource.ImportStateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(typ, nil)}}
		newRes(newFake()).ImportState(context.Background(), resource.ImportStateRequest{ID: id}, imp)
		if !imp.Diagnostics.HasError() {
			t.Fatalf("id %q must be rejected", id)
		}
	}
}

func TestDelete(t *testing.T) {
	f := newFake()
	st := seed(t, f, baseSpec())
	resp := &resource.DeleteResponse{State: st}
	newRes(f).Delete(context.Background(), resource.DeleteRequest{State: st}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("%v", resp.Diagnostics)
	}
	// already gone: 404 is success
	resp = &resource.DeleteResponse{State: st}
	newRes(f).Delete(context.Background(), resource.DeleteRequest{State: st}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("delete 404 must succeed: %v", resp.Diagnostics)
	}
	st = seed(t, f, baseSpec())
	f.fail["DELETE "+delete_rs_authUser] = &apiclient.APIError{StatusCode: 500, Status: "500", Body: "x"}
	resp = &resource.DeleteResponse{State: st}
	newRes(f).Delete(context.Background(), resource.DeleteRequest{State: st}, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("500 must fail")
	}
}

// ---------------------------------------------------------------------------
// Config validation
// ---------------------------------------------------------------------------

func validateCfg(t *testing.T, mut func(*resource_auth_user.AuthUserCustomModel)) bool {
	sr, _ := schemaOf(t)
	s := baseSpec()
	m := s.model(false)
	m.Uuid = types.StringNull()
	mut(&m)
	cfg := tfsdk.Config{Schema: sr.Schema, Raw: rawOf(t, m)}
	resp := &resource.ValidateConfigResponse{}
	newRes(newFake()).ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: cfg}, resp)
	return resp.Diagnostics.HasError()
}

func TestValidateConfig(t *testing.T) {
	if validateCfg(t, func(m *resource_auth_user.AuthUserCustomModel) {}) {
		t.Fatalf("plain config must be valid")
	}
	if !validateCfg(t, func(m *resource_auth_user.AuthUserCustomModel) { m.PasswordWo = types.StringValue("x") }) {
		t.Fatalf("password_wo without version must fail")
	}
	if !validateCfg(t, func(m *resource_auth_user.AuthUserCustomModel) { m.PasswordWoVersion = types.Int64Value(1) }) {
		t.Fatalf("version without password_wo must fail")
	}
	if validateCfg(t, func(m *resource_auth_user.AuthUserCustomModel) {
		m.PasswordWo = types.StringValue("x")
		m.PasswordWoVersion = types.Int64Value(1)
	}) {
		t.Fatalf("pw+version must be valid")
	}
	if !validateCfg(t, func(m *resource_auth_user.AuthUserCustomModel) { m.Groups = listOf([]string{"a", "a"}) }) {
		t.Fatalf("duplicate groups must fail")
	}
}

func TestLegacyPasswordSchemaRejectsValue(t *testing.T) {
	sr, _ := schemaOf(t)
	a := sr.Schema.Attributes["password"]
	if !a.IsSensitive() {
		t.Fatalf("legacy password must be sensitive")
	}
	if !sr.Schema.Attributes["password_wo"].IsWriteOnly() || !sr.Schema.Attributes["password_wo"].IsSensitive() {
		t.Fatalf("password_wo must be write-only + sensitive")
	}
	if sr.Schema.Attributes["password_wo"].IsComputed() {
		t.Fatalf("password_wo must not be computed")
	}
}

func TestUpdateOmittedGroupsStaysUnmanagedDespitePlanValue(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.groups, s.groupsSet = []string{"A"}, true
	st := seed(t, f, s)
	// concurrent out-of-band membership change
	f.users[getModel(t, st).Uuid.ValueString()]["groups"] = []string{"A", "B"}
	s.groupsSet, s.groups, s.first = false, nil, "Changed" // config omits groups; plan carries prior [A]
	resp, st2 := update(t, f, st, s)
	if resp.Diagnostics.HasError() {
		t.Fatalf("%v", resp.Diagnostics)
	}
	if f.has("PUT", groups_rs_authUser) || f.has("DELETE", groups_rs_authUser) {
		t.Fatalf("unmanaged groups must not be reconciled: %v", f.methods())
	}
	if !f.has("PUT", update_rs_authUser) {
		t.Fatalf("identity edit expected")
	}
	if g := groupList(t, getModel(t, st2)); strings.Join(g, ",") != "A,B" {
		t.Fatalf("state must reflect server membership, got %v", g)
	}
}

// ---------------------------------------------------------------------------
// disabled-user password rotation (the reset API may re-enable the account)
// ---------------------------------------------------------------------------

func enabledOf(f *fakeAPI, st tfsdk.State, t *testing.T) bool {
	return f.users[getModel(t, st).Uuid.ValueString()]["enabled"].(bool)
}

func TestCreateDisabledWithPasswordStaysDisabledBeforeGroups(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.enabled, s.pw, s.version, s.groups, s.groupsSet = boolp(false), testSecret, i64(1), []string{"g1"}, true
	resp, st := create(t, f, s)
	if resp.Diagnostics.HasError() {
		t.Fatalf("%v", resp.Diagnostics)
	}
	if enabledOf(f, st, t) || getModel(t, st).Enabled.ValueBool() {
		t.Fatalf("user must end disabled")
	}
	// corrective PUT must happen after the reset and before the membership call
	idx := map[string]int{}
	for i, m := range f.methods() {
		if _, ok := idx[m]; !ok {
			idx[m] = i
		}
	}
	r, c, g := idx["PUT "+resetpassword_rs_authUser], idx["PUT "+update_rs_authUser], idx["PUT "+groups_rs_authUser]
	if !(r < c && c < g) {
		t.Fatalf("order reset<disable<groups violated: %v", f.methods())
	}
}

func TestUpdatePasswordOnlyRotationDisabledUser(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.enabled, s.pw, s.version = boolp(false), testSecret, i64(1)
	st := seed(t, f, s)
	s.pw, s.version = "n-"+testSecret, i64(2)
	resp, st2 := update(t, f, st, s)
	if resp.Diagnostics.HasError() {
		t.Fatalf("%v", resp.Diagnostics)
	}
	if enabledOf(f, st2, t) || getModel(t, st2).Enabled.ValueBool() {
		t.Fatalf("disabled user must stay disabled after rotation")
	}
	if getModel(t, st2).PasswordWoVersion.ValueInt64() != 2 {
		t.Fatalf("version")
	}
}

func TestUpdateDisableAndRotateSimultaneously(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.pw, s.version = testSecret, i64(1)
	st := seed(t, f, s)
	s.enabled, s.pw, s.version = boolp(false), "n-"+testSecret, i64(2)
	resp, st2 := update(t, f, st, s)
	if resp.Diagnostics.HasError() {
		t.Fatalf("%v", resp.Diagnostics)
	}
	if enabledOf(f, st2, t) {
		t.Fatalf("user must end disabled")
	}
}

func TestUpdateRotationThenGroupFailureStillDisabled(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.enabled, s.pw, s.version, s.groups, s.groupsSet = boolp(false), testSecret, i64(1), []string{"g1"}, true
	st := seed(t, f, s)
	f.fail["PUT "+groups_rs_authUser] = fmt.Errorf("boom")
	s.pw, s.version, s.groups = "n-"+testSecret, i64(2), []string{"g2"}
	resp, st2 := update(t, f, st, s)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected error")
	}
	if enabledOf(f, st2, t) {
		t.Fatalf("membership failure must not leave the account enabled")
	}
	if getModel(t, st2).PasswordWoVersion.ValueInt64() != 2 {
		t.Fatalf("version must reflect the applied reset")
	}
}

func TestUpdateCorrectiveDisableFailureFailsClosed(t *testing.T) {
	f := newFake()
	s := baseSpec()
	s.enabled, s.pw, s.version, s.groups, s.groupsSet = boolp(false), testSecret, i64(1), []string{"g1"}, true
	st := seed(t, f, s)
	// the corrective identity PUT (after the reset) fails
	f.failFrom["PUT "+update_rs_authUser] = 1
	s.pw, s.version, s.groups = "n-"+testSecret, i64(2), []string{"g2"}
	resp, st2 := update(t, f, st, s)
	if !resp.Diagnostics.HasError() {
		t.Fatalf("expected error")
	}
	if f.has("PUT", groups_rs_authUser) || f.has("DELETE", groups_rs_authUser) {
		t.Fatalf("no membership call allowed after failed disable: %v", f.methods())
	}
	m := getModel(t, st2)
	if !m.Enabled.ValueBool() || m.PasswordWoVersion.ValueInt64() != 2 {
		t.Fatalf("state must truthfully show enabled=true, version=2: %+v", m)
	}
	// the next plan sees enabled true vs false and the version no longer changes
	if !enabledOf(f, st2, t) {
		t.Fatalf("test premise: server account is enabled")
	}
}

// ---------------------------------------------------------------------------
// protected built-in identities
// ---------------------------------------------------------------------------

const adminUUID = "99999999-9999-9999-9999-999999999999"

func seedAdmin(f *fakeAPI) {
	f.users[adminUUID] = map[string]any{"uuid": adminUUID, "username": "admin", "email": "a@x", "firstName": "a", "lastName": "b", "enabled": true}
}

func aliasedState(t *testing.T, f *fakeAPI) tfsdk.State {
	// state claims a harmless username but the UUID is the real admin
	seedAdmin(f)
	s := baseSpec()
	s.username = "hmedhioub"
	st := seed(t, f, s)
	m := getModel(t, st)
	m.Uuid = types_StringValue(adminUUID)
	return setState(t, m)
}

func types_StringValue(s string) types.String { return types.StringValue(s) }

func setState(t *testing.T, m resource_auth_user.AuthUserCustomModel) tfsdk.State {
	sr, _ := schemaOf(t)
	return tfsdk.State{Schema: sr.Schema, Raw: rawOf(t, m)}
}

func TestImportRejectsProtectedActualIdentity(t *testing.T) {
	f := newFake()
	seedAdmin(f)
	sr, typ := schemaOf(t)
	imp := &resource.ImportStateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(typ, nil)}}
	newRes(f).ImportState(context.Background(), resource.ImportStateRequest{ID: adminUUID}, imp)
	if !imp.Diagnostics.HasError() || !imp.State.Raw.IsNull() {
		t.Fatalf("protected import must fail and save no state: %v", imp.Diagnostics)
	}
	for _, c := range f.calls {
		if c.Method != "GET" {
			t.Fatalf("only reads allowed, got %s %s", c.Method, c.Path)
		}
	}
	// missing user and normal user
	imp = &resource.ImportStateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(typ, nil)}}
	newRes(f).ImportState(context.Background(), resource.ImportStateRequest{ID: "11111111-1111-1111-1111-111111111111"}, imp)
	if !imp.Diagnostics.HasError() {
		t.Fatalf("unknown user import must fail")
	}
}

func TestProtectedActualIdentityBlocksReadUpdateDelete(t *testing.T) {
	f := newFake()
	st := aliasedState(t, f)
	f.calls = nil

	rresp := &resource.ReadResponse{State: st}
	newRes(f).Read(context.Background(), resource.ReadRequest{State: st}, rresp)
	if !rresp.Diagnostics.HasError() || rresp.State.Raw.IsNull() {
		t.Fatalf("read of protected user must error without removing/rewriting state")
	}

	s := baseSpec()
	s.username, s.first, s.pw, s.version = "hmedhioub", "X", "n-"+testSecret, i64(5)
	uresp, _ := update(t, f, st, s)
	if !uresp.Diagnostics.HasError() {
		t.Fatalf("update must be refused")
	}

	dresp := &resource.DeleteResponse{State: st}
	newRes(f).Delete(context.Background(), resource.DeleteRequest{State: st}, dresp)
	if !dresp.Diagnostics.HasError() {
		t.Fatalf("delete must be refused")
	}
	if _, ok := f.users[adminUUID]; !ok {
		t.Fatalf("admin deleted!")
	}
	for _, c := range f.calls {
		if c.Method != "GET" {
			t.Fatalf("no mutation allowed against protected user, got %s %s", c.Method, c.Path)
		}
	}
}

func TestCreateRejectsProtectedUsernames(t *testing.T) {
	for _, n := range []string{"admin", "Admin", " system-administrator "} {
		f := newFake()
		s := baseSpec()
		s.username = n
		resp, _ := create(t, f, s)
		if !resp.Diagnostics.HasError() || len(f.calls) != 0 {
			t.Fatalf("%q: create must be refused without API calls", n)
		}
	}
	if !validateCfg(t, func(m *resource_auth_user.AuthUserCustomModel) { m.Username = types.StringValue("ADMIN") }) {
		t.Fatalf("config validation must reject protected username")
	}
}

func TestDeleteMissingActualUsernameRefused(t *testing.T) {
	f := newFake()
	st := seed(t, f, baseSpec())
	delete(f.users[getModel(t, st).Uuid.ValueString()], "username")
	f.calls = nil
	resp := &resource.DeleteResponse{State: st}
	newRes(f).Delete(context.Background(), resource.DeleteRequest{State: st}, resp)
	if !resp.Diagnostics.HasError() || f.has("DELETE", delete_rs_authUser) {
		t.Fatalf("unknown identity must not be deleted")
	}
}
