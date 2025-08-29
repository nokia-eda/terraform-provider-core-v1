package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/eda/apiclient"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/resource_cluster_auth_role"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/tfutils"
)

const (
	create_rs_clusterAuthRole = "/core/admin/roles"
	read_rs_clusterAuthRole   = "/core/admin/roles/{name}"
	update_rs_clusterAuthRole = "/core/admin/roles/{name}"
	delete_rs_clusterAuthRole = "/core/admin/roles/{name}"
)

var (
	_ resource.Resource                = (*clusterAuthRoleResource)(nil)
	_ resource.ResourceWithConfigure   = (*clusterAuthRoleResource)(nil)
	_ resource.ResourceWithImportState = (*clusterAuthRoleResource)(nil)
)

func NewClusterAuthRoleResource() resource.Resource {
	return &clusterAuthRoleResource{}
}

type clusterAuthRoleResource struct {
	client *apiclient.EdaApiClient
}

func (r *clusterAuthRoleResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cluster_auth_role"
}

func (r *clusterAuthRoleResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resource_cluster_auth_role.ClusterAuthRoleResourceSchema(ctx)
}

func (r *clusterAuthRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data resource_cluster_auth_role.ClusterAuthRoleModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Initialize unknown values with null defaults
	err := tfutils.FillMissingValues(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Error filling missing values", err.Error())
		return
	}

	// Convert Terraform model to API request body
	reqBody, err := tfutils.ModelToAnyMap(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Error building request", err.Error())
		return
	}

	// Create API call logic
	tflog.Info(ctx, "Create()::API request", map[string]any{
		"path": create_rs_clusterAuthRole,
		"body": spew.Sdump(reqBody),
	})

	t0 := time.Now()
	result := map[string]any{}

	err = r.client.Create(ctx, create_rs_clusterAuthRole, nil, reqBody, &result)

	tflog.Info(ctx, "Create()::API returned", map[string]any{
		"path":      create_rs_clusterAuthRole,
		"result":    spew.Sdump(result),
		"timeTaken": time.Since(t0).String(),
	})

	if err != nil {
		resp.Diagnostics.AddError("Error creating resource", err.Error())
		return
	}

	// Read the resource again to populate any values not available in the response from Create()
	t0 = time.Now()

	err = r.client.Get(ctx, read_rs_clusterAuthRole, map[string]string{
		"name": tfutils.StringValue(data.Name),
	}, &result)

	tflog.Info(ctx, "Read()::API returned", map[string]any{
		"path":      read_rs_clusterAuthRole,
		"result":    spew.Sdump(result),
		"timeTaken": time.Since(t0).String(),
	})

	if err != nil {
		resp.Diagnostics.AddError("Error reading resource", err.Error())
		return
	}

	// Convert API response to Terraform model
	err = tfutils.AnyMapToModel(ctx, result, &data)
	if err != nil {
		resp.Diagnostics.AddError("Failed to build response from API result", err.Error())
		return
	}
	// Save created data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, data)...)
}

func (r *clusterAuthRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data resource_cluster_auth_role.ClusterAuthRoleModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Read API call logic
	tflog.Info(ctx, "Read()::API request", map[string]any{
		"path": read_rs_clusterAuthRole,
		"data": spew.Sdump(data),
	})

	t0 := time.Now()
	result := map[string]any{}

	err := r.client.Get(ctx, read_rs_clusterAuthRole, map[string]string{
		"name": tfutils.StringValue(data.Name),
	}, &result)

	tflog.Info(ctx, "Read()::API returned", map[string]any{
		"path":      read_rs_clusterAuthRole,
		"result":    spew.Sdump(result),
		"timeTaken": time.Since(t0).String(),
	})

	if err != nil {
		resp.Diagnostics.AddError("Error reading resource", err.Error())
		return
	}

	// Convert API response to Terraform model
	err = tfutils.AnyMapToModel(ctx, result, &data)
	if err != nil {
		resp.Diagnostics.AddError("Failed to build response from API result", err.Error())
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *clusterAuthRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data resource_cluster_auth_role.ClusterAuthRoleModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	err := tfutils.FillMissingValues(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Error filling missing values", err.Error())
		return
	}

	reqBody, err := tfutils.ModelToAnyMap(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Error building request", err.Error())
		return
	}

	// Update API call logic
	tflog.Info(ctx, "Update()::API request", map[string]any{
		"path": update_rs_clusterAuthRole,
		"body": spew.Sdump(reqBody),
	})

	t0 := time.Now()
	result := map[string]any{}

	err = r.client.Update(ctx, update_rs_clusterAuthRole, map[string]string{
		"name": tfutils.StringValue(data.Name),
	}, reqBody, &result)

	tflog.Info(ctx, "Update()::API returned", map[string]any{
		"path":      update_rs_clusterAuthRole,
		"result":    spew.Sdump(result),
		"timeTaken": time.Since(t0).String(),
	})

	if err != nil {
		resp.Diagnostics.AddError("Error updating resource", err.Error())
		return
	}

	// Read the resource again to populate any values not available in the response from Update()
	t0 = time.Now()

	err = r.client.Get(ctx, read_rs_clusterAuthRole, map[string]string{
		"name": tfutils.StringValue(data.Name),
	}, &result)

	tflog.Info(ctx, "Read()::API returned", map[string]any{
		"path":      read_rs_clusterAuthRole,
		"result":    spew.Sdump(result),
		"timeTaken": time.Since(t0).String(),
	})

	if err != nil {
		resp.Diagnostics.AddError("Error reading resource", err.Error())
		return
	}

	// Convert API response to Terraform model
	err = tfutils.AnyMapToModel(ctx, result, &data)
	if err != nil {
		resp.Diagnostics.AddError("Failed to build response from API result", err.Error())
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *clusterAuthRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data resource_cluster_auth_role.ClusterAuthRoleModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Delete API call logic
	tflog.Info(ctx, "Delete()::API request", map[string]any{
		"path": delete_rs_clusterAuthRole,
		"data": spew.Sdump(data),
	})

	t0 := time.Now()
	result := map[string]any{}

	err := r.client.Delete(ctx, delete_rs_clusterAuthRole, map[string]string{
		"name": tfutils.StringValue(data.Name),
	}, &result)

	tflog.Info(ctx, "Delete()::API returned", map[string]any{
		"path":      delete_rs_clusterAuthRole,
		"result":    spew.Sdump(result),
		"timeTaken": time.Since(t0).String(),
	})

	if err != nil {
		resp.Diagnostics.AddError("Error deleting resource", err.Error())
		return
	}
}

// Configure adds the provider configured client to the resource.
func (r *clusterAuthRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Add a nil check when handling ProviderData because Terraform
	// sets that data after it calls the ConfigureProvider RPC.
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

// ImportState implements resource.ResourceWithImportState.
func (r *clusterAuthRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) < 1 {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Expected <namespace/name> format, got: %s", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[0])...)
}
