package resource_transaction

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type TransactionModel struct {
	Id          types.Int64   `tfsdk:"id"`
	Crs         types.Dynamic `tfsdk:"crs"`
	Description types.String  `tfsdk:"description"`
	DryRun      types.Bool    `tfsdk:"dry_run"`
	ResultType  types.String  `tfsdk:"result_type"`
	Retain      types.Bool    `tfsdk:"retain"`
}

// type TransactionCr struct {
// 	Type TransactionType `tfsdk:"type"`
// }

// type TransactionType struct {
// 	Create  TransactionValue `tfsdk:"create"`
// 	Delete  NsCrGvkName      `tfsdk:"delete"`
// 	Modify  TransactionValue `tfsdk:"modify"`
// 	Patch   TransactionPatch `tfsdk:"patch"`
// 	Replace TransactionValue `tfsdk:"replace"`
// }

// type TransactionValue struct {
// 	Value TransactionContent `tfsdk:"value"`
// }

// type TransactionContent struct {
// 	ApiVersion types.String  `tfsdk:"api_version"`
// 	Kind       types.String  `tfsdk:"kind"`
// 	Metadata   MetadataValue `tfsdk:"metadata"`
// 	Spec       types.Map     `tfsdk:"spec"`
// }

// type MetadataValue struct {
// 	Annotations basetypes.MapValue    `tfsdk:"annotations"`
// 	Labels      basetypes.MapValue    `tfsdk:"labels"`
// 	Name        basetypes.StringValue `tfsdk:"name"`
// 	Namespace   basetypes.StringValue `tfsdk:"namespace"`
// }

// type TransactionPatch struct {
// 	PatchOps []K8SPatchOp `tfsdk:"patch_ops"`
// 	Target   NsCrGvkName  `tfsdk:"target"`
// }

// type K8SPatchOp struct {
// 	From        types.String `tfsdk:"from,omitempty"`
// 	Op          types.String `tfsdk:"op"`
// 	Path        types.String `tfsdk:"path"`
// 	Value       types.Object `tfsdk:"value,omitempty"`
// 	XPermissive types.Bool   `tfsdk:"x_permissive,omitempty"`
// }

// type NsCrGvkName struct {
// 	Gvk       GroupVersionKind `tfsdk:"gvk,omitempty"`
// 	Name      types.String     `tfsdk:"name,omitempty"`
// 	Namespace types.String     `tfsdk:"namespace,omitempty"`
// }

// type GroupVersionKind struct {
// 	Group   types.String `tfsdk:"group,omitempty"`
// 	Kind    types.String `tfsdk:"kind,omitempty"`
// 	Version types.String `tfsdk:"version,omitempty"`
// }

func TransactionResourceSchema(ctx context.Context) schema.Schema {
	return schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Computed:            true,
				Description:         "A transaction identifier; these are assigned by the system to a posted transaction.",
				MarkdownDescription: "A transaction identifier; these are assigned by the system to a posted transaction.",
			},
			"crs": schema.DynamicAttribute{
				Required:            true,
				Description:         "List of CRs to include in the transaction",
				MarkdownDescription: "List of CRs to include in the transaction",
			},
			"description": schema.StringAttribute{
				Required:            true,
				Description:         "Description/commit message for the transaction",
				MarkdownDescription: "Description/commit message for the transaction",
			},
			"dry_run": schema.BoolAttribute{
				Required:            true,
				Description:         "If true the transaction will not be committed and will run in dry run mode.  If false the\ntransaction will be committed",
				MarkdownDescription: "If true the transaction will not be committed and will run in dry run mode.  If false the\ntransaction will be committed",
			},
			"result_type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Description:         "The type of result - errors only, normal, or debug",
				MarkdownDescription: "The type of result - errors only, normal, or debug",
			},
			"retain": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Description:         "retain after results fetched - e.g. after call to get transaction result",
				MarkdownDescription: "retain after results fetched - e.g. after call to get transaction result",
			},
		},
	}
}
