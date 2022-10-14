package collectors

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/zapr"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/spotinst/spotinst-sdk-go/service/mcs"
	"github.com/spotinst/spotinst-sdk-go/service/ocean/providers/aws"
	"github.com/spotinst/spotinst-sdk-go/spotinst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

type mockOceanAWSClusterCostsClient struct {
	mock.Mock
}

func (m *mockOceanAWSClusterCostsClient) GetClusterCosts(
	ctx context.Context,
	input *mcs.ClusterCostInput,
) (*mcs.ClusterCostOutput, error) {
	args := m.Called(ctx, input)
	output := args.Get(0)

	if output == nil {
		return nil, args.Error(1)
	}

	return output.(*mcs.ClusterCostOutput), args.Error(1)
}

func TestOceanAWSClusterCostsCollector(t *testing.T) {
	testCases := []struct {
		name     string
		client   func() OceanAWSClusterCostsClient
		expected string
		clusters []*aws.Cluster
	}{
		{
			name: "no cluster, no output",
			client: func() OceanAWSClusterCostsClient {
				return new(mockOceanAWSClusterCostsClient)
			},
		},
		{
			name: "nonexistent cluster",
			client: func() OceanAWSClusterCostsClient {
				input := clusterCostInput("nonexistent")

				mockClient := new(mockOceanAWSClusterCostsClient)
				mockClient.On("GetClusterCosts", mock.Anything, input).Return(nil, errors.New("nonexistent"))
				return mockClient
			},
			clusters: oceanClusters("nonexistent"),
		},
		{
			name: "one cluster",
			client: func() OceanAWSClusterCostsClient {
				input := clusterCostInput("foo")
				output := clusterCostOutput(
					200,
					namespaceCost("foo-ns", 190, resourceCost("foo-ns", "foo-deployment", 180)),
				)

				mockClient := new(mockOceanAWSClusterCostsClient)
				mockClient.On("GetClusterCosts", mock.Anything, input).Return(output, nil)
				return mockClient
			},
			clusters: oceanClusters("foo"),
			expected: `
                # HELP spotinst_ocean_aws_cluster_cost Total cost of an ocean cluster
                # TYPE spotinst_ocean_aws_cluster_cost gauge
                spotinst_ocean_aws_cluster_cost{ocean_id="foo",ocean_name="ocean-foo"} 200
                # HELP spotinst_ocean_aws_namespace_cost Total cost of a namespace
                # TYPE spotinst_ocean_aws_namespace_cost gauge
                spotinst_ocean_aws_namespace_cost{namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo"} 190
                # HELP spotinst_ocean_aws_deployment_cost Total cost of a deployment
                # TYPE spotinst_ocean_aws_deployment_cost gauge
                spotinst_ocean_aws_deployment_cost{name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo"} 180
            `,
		},
	}

	logger := zapr.NewLogger(zap.NewNop())

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			collector := NewOceanAWSClusterCostsCollector(ctx, logger, testCase.client(), testCase.clusters)

			assert.NoError(t, testutil.CollectAndCompare(collector, strings.NewReader(testCase.expected)))
		})
	}
}

func oceanClusters(clusterIDs ...string) []*aws.Cluster {
	clusters := make([]*aws.Cluster, 0, len(clusterIDs))

	for _, id := range clusterIDs {
		clusters = append(clusters, &aws.Cluster{
			ID:                  spotinst.String(id),
			ControllerClusterID: spotinst.String(id),
			Name:                spotinst.String("ocean-" + id),
		})
	}

	return clusters
}

func clusterCostInput(clusterID string) *mcs.ClusterCostInput {
	now := time.Now()
	firstDayOfCurrentMonth := now.AddDate(0, 0, -now.Day()+1)
	firstDayOfNextMonth := now.AddDate(0, 1, -now.Day()+1)

	return &mcs.ClusterCostInput{
		ClusterID: spotinst.String(clusterID),
		FromDate:  spotinst.String(firstDayOfCurrentMonth.Format("2006-01-02")),
		ToDate:    spotinst.String(firstDayOfNextMonth.Format("2006-01-02")),
	}
}

func clusterCostOutput(cost float64, namespaceCosts ...*mcs.Namespace) *mcs.ClusterCostOutput {
	return &mcs.ClusterCostOutput{
		ClusterCosts: []*mcs.ClusterCost{
			{
				TotalCost:  spotinst.Float64(cost),
				Namespaces: namespaceCosts,
			},
		},
	}
}

func namespaceCost(namespace string, cost float64, resourceCosts ...*mcs.Resource) *mcs.Namespace {
	return &mcs.Namespace{
		Namespace:   spotinst.String(namespace),
		Cost:        spotinst.Float64(cost),
		Deployments: resourceCosts,
	}
}

func resourceCost(namespace, name string, cost float64) *mcs.Resource {
	return &mcs.Resource{
		Name:      spotinst.String(name),
		Namespace: spotinst.String(namespace),
		Cost:      spotinst.Float64(cost),
	}
}
