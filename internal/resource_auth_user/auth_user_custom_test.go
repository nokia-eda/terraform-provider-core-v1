package resource_auth_user

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestLegacyPasswordRejected(t *testing.T) {
	for _, tc := range []struct {
		v    types.String
		fail bool
	}{{types.StringNull(), false}, {types.StringValue("x"), true}} {
		resp := &validator.StringResponse{}
		rejectLegacyPassword{}.ValidateString(context.Background(), validator.StringRequest{ConfigValue: tc.v}, resp)
		if resp.Diagnostics.HasError() != tc.fail {
			t.Fatalf("value %v: error=%v want %v", tc.v, resp.Diagnostics.HasError(), tc.fail)
		}
	}
}

func TestIsUUID(t *testing.T) {
	if !IsUUID("253f388d-dfae-4fb5-8438-75f19e7b075b") || IsUUID("") || IsUUID("admin") {
		t.Fatal("IsUUID wrong")
	}
}
