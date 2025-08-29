package provider

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/eda/apiclient"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/resource_transaction"
	"github.com/nokia/eda/apps/terraform-provider-core/internal/tfutils"
)

const (
	create_transaction = "/core/transaction/v2"
	delete_transaction = "/core/transaction/v2/revert/{transactionId}"
)

var (
	_ resource.Resource              = (*transactionResource)(nil)
	_ resource.ResourceWithConfigure = (*transactionResource)(nil)
	// _ resource.ResourceWithImportState = (*transactionResource)(nil)
)

func NewTransactionResource() resource.Resource {
	return &transactionResource{}
}

type transactionResource struct {
	client *apiclient.EdaApiClient
}

func (r *transactionResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_transaction"
}

func (r *transactionResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = resource_transaction.TransactionResourceSchema(ctx)
}

func (r *transactionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data resource_transaction.TransactionModel

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
	tflog.Info(ctx, "Create()::API request", map[string]any{"body": spew.Sdump(reqBody)})

	t0 := time.Now()
	result := map[string]any{}

	err = r.client.Create(ctx, create_transaction, nil, reqBody, &result)

	tflog.Info(ctx, "Create()::API returned", map[string]any{
		"result":    result,
		"type":      reflect.TypeOf(result["id"]),
		"timeTaken": time.Since(t0).String(),
	})

	if err != nil {
		resp.Diagnostics.AddError("Error creating resource", err.Error())
		return
	}

	// Convert API response to Terraform model
	anyVal, ok := result["id"]
	if !ok {
		resp.Diagnostics.AddError("Failed to build response from API result", "Transaction id missing from result")
		return
	}

	id, err := tfutils.NumToInt64(anyVal)
	if err != nil {
		resp.Diagnostics.AddError("Error parsing transaction id", err.Error())
		return
	}

	// Save created data into Terraform state
	data.Id = types.Int64Value(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, data)...)
}

func (r *transactionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data resource_transaction.TransactionModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Read()", map[string]any{"data": spew.Sdump(data)})

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *transactionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data resource_transaction.TransactionModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Update()", map[string]any{"data": spew.Sdump(data)})

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *transactionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data resource_transaction.TransactionModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Delete API call logic
	tflog.Info(ctx, "Delete()::API request", map[string]any{"id": data.Id})

	t0 := time.Now()
	result := map[string]any{}

	err := r.client.Create(ctx, delete_transaction, map[string]string{
		"transactionId": strconv.FormatInt(data.Id.ValueInt64(), 10),
	}, nil, &result)

	tflog.Info(ctx, "Delete()::API returned", map[string]any{
		"result":    result,
		"timeTaken": time.Since(t0).String(),
	})

	if err != nil {
		resp.Diagnostics.AddError("Error deleting resource", err.Error())
		return
	}
}

// Configure adds the provider configured client to the resource.
func (r *transactionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// // ImportState implements resource.ResourceWithImportState.
// func (r *transactionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
// 	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
// }
