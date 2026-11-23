package provider

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-log/tflogtest"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/eda/apiclient"
)

// These tests exercise the real apiclient/resty HTTP path (login, request
// encoding, error handling, logging) against an httptest server, as opposed
// to the in-memory fakeAPI used by the CRUD tests.

func wireServer(t *testing.T, resetHandler http.HandlerFunc) (*httptest.Server, *[]string) {
	var seen []string
	mux := http.NewServeMux()
	mux.HandleFunc("/core/httpproxy/v1/keycloak/realms/eda/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"wire-access-token","refresh_token":"wire-refresh","expires_in":300}`))
	})
	mux.HandleFunc("/core/admin/users", func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"uuid":"11111111-1111-1111-1111-111111111111","username":"tftest-a","email":"a@example.invalid","firstName":"A","lastName":"B","enabled":true,"status":{}}`))
	})
	mux.HandleFunc("/core/admin/users/11111111-1111-1111-1111-111111111111/resetpassword", func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		resetHandler(w, r)
	})
	mux.HandleFunc("/core/admin/users/11111111-1111-1111-1111-111111111111", func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"uuid":"11111111-1111-1111-1111-111111111111","username":"tftest-a","email":"a@example.invalid","firstName":"A","lastName":"B","enabled":true,"status":{}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &seen
}

func wireClient(t *testing.T, ctx context.Context, url string, debug bool) *apiclient.EdaApiClient {
	c, err := apiclient.NewEdaApiClient(ctx, &apiclient.Config{
		BaseURL: url, EdaRealm: "eda", EdaClientID: "eda", EdaClientSecret: "client-secret-value",
		EdaUsername: "admin", EdaPassword: "admin-pass-value",
		RestDebug: debug, RestTimeout: 2 * time.Second, RestRetries: 0, RestRetryInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func wireCreate(t *testing.T, ctx context.Context, c *apiclient.EdaApiClient) *resource.CreateResponse {
	sr, typ := schemaOf(t)
	s := baseSpec()
	s.pw, s.version = testSecret, i64(1)
	cfg := s.model(false)
	cfg.PasswordWo = types.StringValue(testSecret)
	req := resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: sr.Schema, Raw: rawOf(t, s.model(true))},
		Config: tfsdk.Config{Schema: sr.Schema, Raw: rawOf(t, cfg)},
	}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(typ, nil)}}
	newRes(c).Create(ctx, req, resp)
	return resp
}

func TestWireResetPasswordErrorBodyEchoIsNotReported(t *testing.T) {
	var logs bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &logs)
	var gotBody string
	srv, seen := wireServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"message":"invalid password","request":` + string(b) + `}`)) // echoes the request
	})
	resp := wireCreate(t, ctx, wireClient(t, ctx, srv.URL, false))
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected error")
	}
	if !strings.Contains(gotBody, testSecret) {
		t.Fatalf("server did not receive the password in the body: %s", gotBody) // proves wire path
	}
	if strings.Join(*seen, ",") != "POST /core/admin/users,PUT /core/admin/users/11111111-1111-1111-1111-111111111111/resetpassword" {
		t.Fatalf("wire call order: %v", *seen)
	}
	if out := resp.Diagnostics.Errors()[0].Summary() + resp.Diagnostics.Errors()[0].Detail(); strings.Contains(out, testSecret) {
		t.Fatalf("diagnostic leaks secret: %s", out)
	} else if !strings.Contains(out, "400") {
		t.Fatalf("diagnostic should carry the status code: %s", out)
	}
	if strings.Contains(logs.String(), testSecret) || strings.Contains(resp.State.Raw.String(), testSecret) {
		t.Fatal("secret in logs or state")
	}
}

func TestWireResetPasswordTransportErrorIsMasked(t *testing.T) {
	var logs bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &logs)
	srv, _ := wireServer(t, func(w http.ResponseWriter, r *http.Request) {
		hj := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		_ = conn.Close() // transport failure
	})
	resp := wireCreate(t, ctx, wireClient(t, ctx, srv.URL, false))
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected error")
	}
	if strings.Contains(resp.Diagnostics.Errors()[0].Detail(), testSecret) || strings.Contains(logs.String(), testSecret) {
		t.Fatal("secret leaked")
	}
}

func TestResetPasswordMasksSecretInArbitraryError(t *testing.T) {
	r := newRes(errAPI{err: errString("dial failed while sending " + testSecret)})
	err := r.resetPassword(context.Background(), "u", testSecret)
	if err == nil || strings.Contains(err.Error(), testSecret) {
		t.Fatalf("not masked: %v", err)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

type errAPI struct{ err error }

func (e errAPI) Execute(ctx context.Context, p, m string, pp, qp map[string]string, body, result any) error {
	return e.err
}

// resty debug logging goes to stderr: the request/response bodies (password,
// tokens, client secret) and Authorization header must be redacted.
func TestWireRestDebugRedactsSecrets(t *testing.T) {
	old := os.Stderr
	rd, wr, _ := os.Pipe()
	os.Stderr = wr
	var logs bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &logs)
	srv, _ := wireServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(204)
	})
	c := wireClient(t, ctx, srv.URL, true) // resty logger binds os.Stderr at creation
	resp := wireCreate(t, ctx, c)
	_ = wr.Close()
	os.Stderr = old
	out, _ := io.ReadAll(rd)
	if resp.Diagnostics.HasError() {
		t.Fatalf("%v", resp.Diagnostics)
	}
	if len(out) == 0 {
		t.Fatal("expected resty debug output")
	}
	for _, s := range []string{testSecret, "wire-access-token", "wire-refresh", "client-secret-value", "admin-pass-value"} {
		if strings.Contains(string(out), s) || strings.Contains(logs.String(), s) {
			t.Fatalf("debug/log output leaks %q", s)
		}
	}
}
