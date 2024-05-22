package collectors

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Bonial-International-GmbH/spotinst-metrics-exporter/pkg/labels"
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
		name          string
		client        func() OceanAWSClusterCostsClient
		expected      string
		labelMappings labels.Mappings
		clusters      []*aws.Cluster
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
                # HELP spotinst_ocean_aws_workload_cost Total cost of a workload
                # TYPE spotinst_ocean_aws_workload_cost gauge
                spotinst_ocean_aws_workload_cost{name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 180
            `,
		},
		{
			name: "propagate labels",
			client: func() OceanAWSClusterCostsClient {
				input := clusterCostInput("foo")
				output := clusterCostOutput(
					200,
					namespaceCostLabels(
						"foo-ns",
						190,
						map[string]string{
							"team": "foo-team",
						},
						resourceCostLabels("foo-ns", "foo-deployment", 180, map[string]string{
							"team":                   "foo-team",
							"app.kubernetes.io/name": "foo",
						}),
					),
					namespaceCost(
						"other-ns",
						191,
						resourceCostLabels("other-ns", "other-deployment", 181, map[string]string{
							"team": "other-team",
						}),
					),
				)

				mockClient := new(mockOceanAWSClusterCostsClient)
				mockClient.On("GetClusterCosts", mock.Anything, input).Return(output, nil)
				return mockClient
			},
			clusters: oceanClusters("foo"),
			labelMappings: func() labels.Mappings {
				mappings, _ := labels.ParseMappings("team,app.kubernetes.io/name=app")
				return mappings
			}(),
			expected: `
                # HELP spotinst_ocean_aws_cluster_cost Total cost of an ocean cluster
                # TYPE spotinst_ocean_aws_cluster_cost gauge
                spotinst_ocean_aws_cluster_cost{ocean_id="foo",ocean_name="ocean-foo"} 200
                # HELP spotinst_ocean_aws_namespace_cost Total cost of a namespace
                # TYPE spotinst_ocean_aws_namespace_cost gauge
                spotinst_ocean_aws_namespace_cost{app="",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",team="foo-team"} 190
                spotinst_ocean_aws_namespace_cost{app="",namespace="other-ns",ocean_id="foo",ocean_name="ocean-foo",team=""} 191
                # HELP spotinst_ocean_aws_workload_cost Total cost of a workload
                # TYPE spotinst_ocean_aws_workload_cost gauge
                spotinst_ocean_aws_workload_cost{app="foo",name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",team="foo-team",workload="deployment"} 180
                spotinst_ocean_aws_workload_cost{app="",name="other-deployment",namespace="other-ns",ocean_id="foo",ocean_name="ocean-foo",team="other-team",workload="deployment"} 181
            `,
		},
	}

	logger := zapr.NewLogger(zap.NewNop())

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			collector := NewOceanAWSClusterCostsCollector(ctx, logger, testCase.client(), testCase.clusters, testCase.labelMappings)

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

func namespaceCostLabels(namespace string, cost float64, labels map[string]string, resourceCosts ...*mcs.Resource) *mcs.Namespace {
	return &mcs.Namespace{
		Namespace:   spotinst.String(namespace),
		Cost:        spotinst.Float64(cost),
		Labels:      labels,
		Deployments: resourceCosts,
	}
}

func namespaceCost(namespace string, cost float64, resourceCosts ...*mcs.Resource) *mcs.Namespace {
	return namespaceCostLabels(namespace, cost, nil, resourceCosts...)
}

func resourceCostLabels(namespace, name string, cost float64, labels map[string]string) *mcs.Resource {
	return &mcs.Resource{
		Name:      spotinst.String(name),
		Namespace: spotinst.String(namespace),
		Cost:      spotinst.Float64(cost),
		Labels:    labels,
	}
}

func resourceCost(namespace, name string, cost float64) *mcs.Resource {
	return resourceCostLabels(namespace, name, cost, nil)
}

func TestAggregateHighCardinalityResources(t *testing.T) {
	resources := []*mcs.Resource{
		resourceCost("foo-ns", "foo", 1),
		resourceCost("foo-ns", "foo-job-27745697", 1),
		resourceCost("foo-ns", "baz-job-0e6b40aa-ffa2-4288-80ba-891fbad4b0ba", 20),
		resourceCost("foo-ns", "foo-job-27745937", 2),
		resourceCost("foo-ns", "baz-job-0e11d9c9-bcb9-4c71-b99e-afcecb5e5fc5", 10),
		resourceCost("foo-ns", "bar", 3),
		resourceCost("foo-ns", "0e11d9c9-bcb9-4c71-b99e-afcecb5e5fc5-qux-job", 5),
		resourceCost("foo-ns", "0e6b40aa-ffa2-4288-80ba-891fbad4b0ba-qux-job", 7),
		resourceCost("foo-ns", "27745697-bam-job", 3),
		resourceCost("foo-ns", "27745937-bam-job", 4),
	}

	expected := []*mcs.Resource{
		resourceCost("foo-ns", "foo", 1),
		resourceCost("foo-ns", "foo-job", 3),
		resourceCost("foo-ns", "bar", 3),
		resourceCost("foo-ns", "baz-job", 30),
		resourceCost("foo-ns", "qux-job", 12),
		resourceCost("foo-ns", "bam-job", 7),
	}

	assert.ElementsMatch(t, expected, aggregateHighCardinalityResources(resources))
}
