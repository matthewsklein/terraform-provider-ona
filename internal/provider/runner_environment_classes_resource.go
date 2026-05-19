package provider

import (
	"context"

	gitpod "github.com/gitpod-io/gitpod-sdk-go"
	"github.com/gitpod-io/gitpod-sdk-go/shared"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &runnerEnvironmentClassesResource{}
	_ resource.ResourceWithImportState = &runnerEnvironmentClassesResource{}
)

type runnerEnvironmentClassesResource struct {
	client *gitpod.Client
}

func NewRunnerEnvironmentClassesResource() resource.Resource {
	return &runnerEnvironmentClassesResource{}
}

type runnerEnvironmentClassesModel struct {
	ID            types.String `tfsdk:"id"`
	RunnerID      types.String `tfsdk:"runner_id"`
	DisplayName   types.String `tfsdk:"display_name"`
	Description   types.String `tfsdk:"description"`
	Configuration types.Map    `tfsdk:"configuration"`
	Enabled       types.Bool   `tfsdk:"enabled"`
}

func (r *runnerEnvironmentClassesResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_runner_environment_classes"
}

func (r *runnerEnvironmentClassesResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `Manages an environment class on a Gitpod runner.

**Supported Runner Types**: This resource supports AWS EC2 (RUNNER_PROVIDER_AWS_EC2) and Managed (RUNNER_PROVIDER_MANAGED) runners only.

**Note**: Environment classes cannot be deleted via the API. Instead, they are disabled to prevent their use.`,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Environment class ID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"runner_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Runner ID this environment class belongs to.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"display_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human-readable environment class name.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Human-readable environment class description.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"configuration": schema.MapAttribute{
				ElementType: types.StringType,
				Required:    true,
				MarkdownDescription: `Configuration values keyed by configuration name. Changing this requires replacement.

  Valid keys for both AWS EC2 and Managed runners:
  - ` + "`instanceType`" + ` (required)
  - ` + "`diskSizeGB`" + ` (optional)
  - ` + "`spot`" + ` (optional)

  **Note**: The API silently ignores invalid configuration keys.`,
				PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"enabled": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Whether the environment class can be used to create new environments.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *runnerEnvironmentClassesResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, ok := clientFromProviderData(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}

	r.client = client
}

func (r *runnerEnvironmentClassesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan runnerEnvironmentClassesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Convert configuration map to key-value pairs
	configMap := make(map[string]string)
	resp.Diagnostics.Append(plan.Configuration.ElementsAs(ctx, &configMap, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	configFields := make([]shared.FieldValueParam, 0, len(configMap))
	for key, value := range configMap {
		configFields = append(configFields, shared.FieldValueParam{
			Key:   gitpod.F(key),
			Value: gitpod.F(value),
		})
	}

	params := gitpod.RunnerConfigurationEnvironmentClassNewParams{
		RunnerID:      gitpod.F(plan.RunnerID.ValueString()),
		DisplayName:   gitpod.F(plan.DisplayName.ValueString()),
		Configuration: gitpod.F(configFields),
	}

	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		params.Description = gitpod.F(plan.Description.ValueString())
	}

	createResp, err := r.client.Runners.Configurations.EnvironmentClasses.New(ctx, params)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create environment class", err.Error())
		return
	}

	// Create only returns ID; read back for full state.
	getResp, err := r.client.Runners.Configurations.EnvironmentClasses.Get(ctx, gitpod.RunnerConfigurationEnvironmentClassGetParams{
		EnvironmentClassID: gitpod.F(createResp.ID),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to read environment class after create", err.Error())
		return
	}

	state, diags := mapEnvironmentClassToModel(getResp.EnvironmentClass)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *runnerEnvironmentClassesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state runnerEnvironmentClassesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	getResp, err := r.client.Runners.Configurations.EnvironmentClasses.Get(ctx, gitpod.RunnerConfigurationEnvironmentClassGetParams{
		EnvironmentClassID: gitpod.F(state.ID.ValueString()),
	})
	if err != nil {
		if isAPINotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}

		resp.Diagnostics.AddError("Failed to read environment class", err.Error())
		return
	}

	newState, diags := mapEnvironmentClassToModel(getResp.EnvironmentClass)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *runnerEnvironmentClassesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan runnerEnvironmentClassesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var prior runnerEnvironmentClassesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	params := gitpod.RunnerConfigurationEnvironmentClassUpdateParams{
		EnvironmentClassID: gitpod.F(prior.ID.ValueString()),
		DisplayName:        gitpod.F(plan.DisplayName.ValueString()),
	}

	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		params.Description = gitpod.F(plan.Description.ValueString())
	}
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		params.Enabled = gitpod.F(plan.Enabled.ValueBool())
	}

	_, err := r.client.Runners.Configurations.EnvironmentClasses.Update(ctx, params)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update environment class", err.Error())
		return
	}

	// Update returns empty; read back for updated state.
	getResp, err := r.client.Runners.Configurations.EnvironmentClasses.Get(ctx, gitpod.RunnerConfigurationEnvironmentClassGetParams{
		EnvironmentClassID: gitpod.F(prior.ID.ValueString()),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to read environment class after update", err.Error())
		return
	}

	state, diags := mapEnvironmentClassToModel(getResp.EnvironmentClass)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *runnerEnvironmentClassesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state runnerEnvironmentClassesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The API does not support deleting environment classes.
	// Instead, disable the environment class to prevent it from being used.
	_, err := r.client.Runners.Configurations.EnvironmentClasses.Update(ctx, gitpod.RunnerConfigurationEnvironmentClassUpdateParams{
		EnvironmentClassID: gitpod.F(state.ID.ValueString()),
		Enabled:            gitpod.F(false),
	})
	if err != nil {
		if isAPINotFound(err) {
			return
		}

		resp.Diagnostics.AddError("Failed to disable environment class", err.Error())
	}
}

func (r *runnerEnvironmentClassesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapEnvironmentClassToModel(environmentClass gitpod.EnvironmentClass) (runnerEnvironmentClassesModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	configurationValues := make(map[string]attr.Value, len(environmentClass.Configuration))
	for _, field := range environmentClass.Configuration {
		configurationValues[field.Key] = types.StringValue(field.Value)
	}

	configuration, configDiags := types.MapValue(types.StringType, configurationValues)
	diags.Append(configDiags...)

	return runnerEnvironmentClassesModel{
		ID:            types.StringValue(environmentClass.ID),
		RunnerID:      types.StringValue(environmentClass.RunnerID),
		DisplayName:   stringValueOrNull(environmentClass.DisplayName),
		Description:   stringValueOrNull(environmentClass.Description),
		Configuration: configuration,
		Enabled:       types.BoolValue(environmentClass.Enabled),
	}, diags
}
