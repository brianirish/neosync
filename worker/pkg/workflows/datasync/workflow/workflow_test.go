package datasync_workflow

import (
	"context"
	"errors"
	"testing"

	tabledependency "github.com/nucleuscloud/neosync/backend/pkg/table-dependency"
	neosync_benthos "github.com/nucleuscloud/neosync/worker/internal/benthos"
	datasync_activities "github.com/nucleuscloud/neosync/worker/pkg/workflows/datasync/activities"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.uber.org/atomic"
)

func Test_Workflow_BenthosConfigsFails(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	activities := &datasync_activities.Activities{}
	env.OnActivity(activities.GenerateBenthosConfigs, mock.Anything, mock.Anything, mock.Anything).Return(nil, errors.New("TestFailure"))

	env.ExecuteWorkflow(Workflow, &WorkflowRequest{})

	assert.True(t, env.IsWorkflowCompleted())
	assert.True(t, env.IsWorkflowCompleted())

	err := env.GetWorkflowError()
	assert.Error(t, err)
	var applicationErr *temporal.ApplicationError
	assert.True(t, errors.As(err, &applicationErr))
	assert.Equal(t, "TestFailure", applicationErr.Error())

	env.AssertExpectations(t)
}

func Test_Workflow_Succeeds_Zero_BenthosConfigs(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	activities := &datasync_activities.Activities{}
	env.OnActivity(activities.GenerateBenthosConfigs, mock.Anything, mock.Anything, mock.Anything).
		Return(&datasync_activities.GenerateBenthosConfigsResponse{BenthosConfigs: []*datasync_activities.BenthosConfigResponse{}}, nil)

	env.ExecuteWorkflow(Workflow, &WorkflowRequest{})

	assert.True(t, env.IsWorkflowCompleted())

	err := env.GetWorkflowError()
	assert.Nil(t, err)

	result := &WorkflowResponse{}
	err = env.GetWorkflowResult(result)
	assert.Nil(t, err)
	assert.Equal(t, result, &WorkflowResponse{})

	env.AssertExpectations(t)
}

func Test_Workflow_Succeeds_SingleSync(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	activities := &datasync_activities.Activities{}
	env.OnActivity(activities.GenerateBenthosConfigs, mock.Anything, mock.Anything, mock.Anything).
		Return(&datasync_activities.GenerateBenthosConfigsResponse{BenthosConfigs: []*datasync_activities.BenthosConfigResponse{
			{
				Name:      "public.users",
				DependsOn: []*tabledependency.DependsOn{},
				Config:    &neosync_benthos.BenthosConfig{},
			},
		}}, nil)
	env.OnActivity(activities.RunSqlInitTableStatements, mock.Anything, mock.Anything).
		Return(&datasync_activities.RunSqlInitTableStatementsResponse{}, nil)
	env.OnActivity(activities.Sync, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&datasync_activities.SyncResponse{}, nil)

	env.ExecuteWorkflow(Workflow, &WorkflowRequest{})

	assert.True(t, env.IsWorkflowCompleted())

	err := env.GetWorkflowError()
	assert.Nil(t, err)

	result := &WorkflowResponse{}
	err = env.GetWorkflowResult(result)
	assert.Nil(t, err)
	assert.Equal(t, result, &WorkflowResponse{})

	env.AssertExpectations(t)
}

func Test_Workflow_Follows_Synchronous_DependentFlow(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	activities := &datasync_activities.Activities{}
	env.OnActivity(activities.GenerateBenthosConfigs, mock.Anything, mock.Anything, mock.Anything).
		Return(&datasync_activities.GenerateBenthosConfigsResponse{BenthosConfigs: []*datasync_activities.BenthosConfigResponse{
			{
				Name:      "public.users",
				DependsOn: []*tabledependency.DependsOn{},
				Config: &neosync_benthos.BenthosConfig{
					StreamConfig: neosync_benthos.StreamConfig{
						Input: &neosync_benthos.InputConfig{
							Inputs: neosync_benthos.Inputs{
								SqlSelect: &neosync_benthos.SqlSelect{
									Columns: []string{"id"},
								},
							},
						},
					},
				},
				TableSchema: "public",
				TableName:   "users",
				Columns:     []string{"id"},
			},
			{
				Name:      "public.foo",
				DependsOn: []*tabledependency.DependsOn{{Table: "public.users", Columns: []string{"id"}}},
				Config: &neosync_benthos.BenthosConfig{
					StreamConfig: neosync_benthos.StreamConfig{
						Input: &neosync_benthos.InputConfig{
							Inputs: neosync_benthos.Inputs{
								SqlSelect: &neosync_benthos.SqlSelect{
									Columns: []string{"id"},
								},
							},
						},
					},
				},
				TableSchema: "public",
				TableName:   "foo",
				Columns:     []string{"id"},
			},
		}}, nil)
	env.OnActivity(activities.RunSqlInitTableStatements, mock.Anything, mock.Anything).
		Return(&datasync_activities.RunSqlInitTableStatementsResponse{}, nil)
	count := 0
	env.
		OnActivity(activities.Sync, mock.Anything, mock.Anything, &datasync_activities.SyncMetadata{Schema: "public", Table: "users"}, mock.Anything).
		Return(func(ctx context.Context, req *datasync_activities.SyncRequest, metadata *datasync_activities.SyncMetadata, workflowMetadata *datasync_activities.WorkflowMetadata) (*datasync_activities.SyncResponse, error) {
			assert.Equal(t, count, 0)
			count += 1
			return &datasync_activities.SyncResponse{}, nil
		})
	env.
		OnActivity(activities.Sync, mock.Anything, mock.Anything, &datasync_activities.SyncMetadata{Schema: "public", Table: "foo"}, mock.Anything).
		Return(func(ctx context.Context, req *datasync_activities.SyncRequest, metadata *datasync_activities.SyncMetadata, workflowMetadata *datasync_activities.WorkflowMetadata) (*datasync_activities.SyncResponse, error) {
			assert.Equal(t, count, 1)
			count += 1
			return &datasync_activities.SyncResponse{}, nil
		})

	env.ExecuteWorkflow(Workflow, &WorkflowRequest{})

	assert.True(t, env.IsWorkflowCompleted())
	assert.Equal(t, count, 2)

	err := env.GetWorkflowError()
	assert.Nil(t, err)

	result := &WorkflowResponse{}
	err = env.GetWorkflowResult(result)
	assert.Nil(t, err)
	assert.Equal(t, result, &WorkflowResponse{})

	env.AssertExpectations(t)
}

func Test_Workflow_Follows_Multiple_Dependents(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	activities := &datasync_activities.Activities{}
	env.OnActivity(activities.GenerateBenthosConfigs, mock.Anything, mock.Anything, mock.Anything).
		Return(&datasync_activities.GenerateBenthosConfigsResponse{BenthosConfigs: []*datasync_activities.BenthosConfigResponse{
			{
				Name:        "public.users",
				DependsOn:   []*tabledependency.DependsOn{},
				TableSchema: "public",
				TableName:   "users",
				Columns:     []string{"id"},
				Config: &neosync_benthos.BenthosConfig{
					StreamConfig: neosync_benthos.StreamConfig{
						Input: &neosync_benthos.InputConfig{
							Inputs: neosync_benthos.Inputs{
								SqlSelect: &neosync_benthos.SqlSelect{
									Columns: []string{"id"},
								},
							},
						},
					},
				},
			},
			{
				Name:        "public.accounts",
				DependsOn:   []*tabledependency.DependsOn{},
				Columns:     []string{"id"},
				TableSchema: "public",
				TableName:   "accounts",
				Config: &neosync_benthos.BenthosConfig{
					StreamConfig: neosync_benthos.StreamConfig{
						Input: &neosync_benthos.InputConfig{
							Inputs: neosync_benthos.Inputs{
								SqlSelect: &neosync_benthos.SqlSelect{
									Columns: []string{"id"},
								},
							},
						},
					},
				},
			},
			{
				Name:        "public.foo",
				DependsOn:   []*tabledependency.DependsOn{{Table: "public.users", Columns: []string{"id"}}, {Table: "public.accounts", Columns: []string{"id"}}},
				Columns:     []string{"id"},
				TableSchema: "public",
				TableName:   "foo",
				Config: &neosync_benthos.BenthosConfig{
					StreamConfig: neosync_benthos.StreamConfig{
						Input: &neosync_benthos.InputConfig{
							Inputs: neosync_benthos.Inputs{
								SqlSelect: &neosync_benthos.SqlSelect{
									Columns: []string{"id"},
								},
							},
						},
					},
				},
			},
		}}, nil)
	env.OnActivity(activities.RunSqlInitTableStatements, mock.Anything, mock.Anything).
		Return(&datasync_activities.RunSqlInitTableStatementsResponse{}, nil)
	counter := atomic.NewInt32(0)
	env.
		OnActivity(activities.Sync, mock.Anything, mock.Anything, &datasync_activities.SyncMetadata{Schema: "public", Table: "users"}, mock.Anything).
		Return(func(ctx context.Context, req *datasync_activities.SyncRequest, metadata *datasync_activities.SyncMetadata, workflowMetadata *datasync_activities.WorkflowMetadata) (*datasync_activities.SyncResponse, error) {
			counter.Add(1)
			return &datasync_activities.SyncResponse{}, nil
		})
	env.
		OnActivity(activities.Sync, mock.Anything, mock.Anything, &datasync_activities.SyncMetadata{Schema: "public", Table: "accounts"}, mock.Anything).
		Return(func(ctx context.Context, req *datasync_activities.SyncRequest, metadata *datasync_activities.SyncMetadata, workflowMetadata *datasync_activities.WorkflowMetadata) (*datasync_activities.SyncResponse, error) {
			counter.Add(1)
			return &datasync_activities.SyncResponse{}, nil
		})
	env.
		OnActivity(activities.Sync, mock.Anything, mock.Anything, &datasync_activities.SyncMetadata{Schema: "public", Table: "foo"}, mock.Anything).
		Return(func(ctx context.Context, req *datasync_activities.SyncRequest, metadata *datasync_activities.SyncMetadata, workflowMetadata *datasync_activities.WorkflowMetadata) (*datasync_activities.SyncResponse, error) {
			assert.Equal(t, counter.Load(), int32(2))
			counter.Add(1)
			return &datasync_activities.SyncResponse{}, nil
		})

	env.ExecuteWorkflow(Workflow, &WorkflowRequest{})

	assert.True(t, env.IsWorkflowCompleted())
	assert.Equal(t, counter.Load(), int32(3))

	err := env.GetWorkflowError()
	assert.Nil(t, err)

	result := &WorkflowResponse{}
	err = env.GetWorkflowResult(result)
	assert.Nil(t, err)
	assert.Equal(t, result, &WorkflowResponse{})

	env.AssertExpectations(t)
}

func Test_Workflow_Halts_Activities_OnError(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	activities := &datasync_activities.Activities{}
	env.OnActivity(activities.GenerateBenthosConfigs, mock.Anything, mock.Anything, mock.Anything).
		Return(&datasync_activities.GenerateBenthosConfigsResponse{BenthosConfigs: []*datasync_activities.BenthosConfigResponse{
			{
				Name:        "public.users",
				DependsOn:   []*tabledependency.DependsOn{},
				Columns:     []string{"id"},
				TableSchema: "public",
				TableName:   "users",
				Config: &neosync_benthos.BenthosConfig{
					StreamConfig: neosync_benthos.StreamConfig{
						Input: &neosync_benthos.InputConfig{
							Inputs: neosync_benthos.Inputs{
								SqlSelect: &neosync_benthos.SqlSelect{
									Columns: []string{"id"},
								},
							},
						},
					},
				},
			},
			{
				Name:        "public.accounts",
				DependsOn:   []*tabledependency.DependsOn{},
				Columns:     []string{"id"},
				TableSchema: "public",
				TableName:   "accounts",
				Config: &neosync_benthos.BenthosConfig{
					StreamConfig: neosync_benthos.StreamConfig{
						Input: &neosync_benthos.InputConfig{
							Inputs: neosync_benthos.Inputs{
								SqlSelect: &neosync_benthos.SqlSelect{
									Columns: []string{"id"},
								},
							},
						},
					},
				},
			},
			{
				Name:        "public.foo",
				DependsOn:   []*tabledependency.DependsOn{{Table: "public.users", Columns: []string{"id"}}, {Table: "public.accounts", Columns: []string{"id"}}},
				Columns:     []string{"id"},
				TableSchema: "public",
				TableName:   "foo",
				Config: &neosync_benthos.BenthosConfig{
					StreamConfig: neosync_benthos.StreamConfig{
						Input: &neosync_benthos.InputConfig{
							Inputs: neosync_benthos.Inputs{
								SqlSelect: &neosync_benthos.SqlSelect{
									Columns: []string{"id"},
								},
							},
						},
					},
				},
			},
		}}, nil)
	env.OnActivity(activities.RunSqlInitTableStatements, mock.Anything, mock.Anything).
		Return(&datasync_activities.RunSqlInitTableStatementsResponse{}, nil)

	env.
		OnActivity(activities.Sync, mock.Anything, mock.Anything, &datasync_activities.SyncMetadata{Schema: "public", Table: "users"}, mock.Anything).
		Return(func(ctx context.Context, req *datasync_activities.SyncRequest, metadata *datasync_activities.SyncMetadata, workflowMetadata *datasync_activities.WorkflowMetadata) (*datasync_activities.SyncResponse, error) {
			return &datasync_activities.SyncResponse{}, nil
		})
	env.
		OnActivity(activities.Sync, mock.Anything, mock.Anything, &datasync_activities.SyncMetadata{Schema: "public", Table: "accounts"}, mock.Anything).
		Return(nil, errors.New("TestFailure"))

	env.ExecuteWorkflow(Workflow, &WorkflowRequest{})

	assert.True(t, env.IsWorkflowCompleted())

	err := env.GetWorkflowError()
	assert.Error(t, err)
	var applicationErr *temporal.ApplicationError
	assert.True(t, errors.As(err, &applicationErr))
	assert.Equal(t, "TestFailure", applicationErr.Error())

	env.AssertExpectations(t)
}

func Test_isConfigReady(t *testing.T) {
	assert.False(t, isConfigReady(nil, nil), "config is nil")
	assert.True(
		t,
		isConfigReady(
			&datasync_activities.BenthosConfigResponse{
				Name:      "foo",
				DependsOn: []*tabledependency.DependsOn{},
			},
			nil,
		),
		"has no dependencies",
	)

	assert.False(
		t,
		isConfigReady(
			&datasync_activities.BenthosConfigResponse{
				Name:      "foo",
				DependsOn: []*tabledependency.DependsOn{{Table: "bar", Columns: []string{"id"}}, {Table: "baz", Columns: []string{"id"}}},
			},
			map[string][]string{
				"bar": {"id"},
			},
		),
		"not all dependencies are finished",
	)

	assert.True(
		t,
		isConfigReady(
			&datasync_activities.BenthosConfigResponse{
				Name:      "foo",
				DependsOn: []*tabledependency.DependsOn{{Table: "bar", Columns: []string{"id"}}, {Table: "baz", Columns: []string{"id"}}},
			},
			map[string][]string{
				"bar": {"id"},
				"baz": {"id"},
			},
		),
		"all dependencies are finished",
	)

	assert.False(
		t,
		isConfigReady(
			&datasync_activities.BenthosConfigResponse{
				Name:      "foo",
				DependsOn: []*tabledependency.DependsOn{{Table: "bar", Columns: []string{"id", "f_id"}}},
			},
			map[string][]string{
				"bar": {"id"},
			},
		),
		"not all dependencies columns are finished",
	)
}
