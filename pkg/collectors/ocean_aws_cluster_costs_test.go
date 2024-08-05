package collectors

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Bonial-International-GmbH/spotinst-metrics-exporter/pkg/labels"
	"github.com/go-logr/zapr"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/spotinst/spotinst-sdk-go/service/ocean/providers/aws"
	"github.com/spotinst/spotinst-sdk-go/spotinst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

type mockOceanAWSClusterCostsClient struct {
	mock.Mock
}

type mockLabelCache struct {
	mock.Mock
}

func (m *mockLabelCache) PopulateOnce()   {}
func (m *mockLabelCache) PopulationLoop() {}

func (m *mockLabelCache) GetLabelFor(
	ctx context.Context,
	resourceType string,
	namespace string,
	cluster string,
	resourceIdentifier string,

) (map[string]string, error) {
	args := m.Called(ctx, resourceType, namespace, cluster, resourceIdentifier)
	output := args.Get(0)

	if output == nil {
		return nil, args.Error(1)
	}

	return output.(map[string]string), args.Error(1)
}

func (m *mockOceanAWSClusterCostsClient) GetClusterAggregatedCosts(
	ctx context.Context,
	input *aws.ClusterAggregatedCostInput,
) (*aws.ClusterAggregatedCostOutput, error) {
	args := m.Called(ctx, input)
	output := args.Get(0)

	if output == nil {
		return nil, args.Error(1)
	}

	return output.(*aws.ClusterAggregatedCostOutput), args.Error(1)
}

func TestOceanAWSClusterCostsCollector(t *testing.T) {
	testCases := []struct {
		name          string
		client        func() OceanAWSClusterCostsClient
		labelCache    func() K8sLabelRetriever
		expected      string
		labelMappings labels.Mappings
		clusters      []*aws.Cluster
	}{
		{
			name: "no cluster, no output",
			client: func() OceanAWSClusterCostsClient {
				return new(mockOceanAWSClusterCostsClient)
			},
			labelCache: func() K8sLabelRetriever {
				return new(mockLabelCache)
			},
		},
		{
			name: "nonexistent cluster",
			labelCache: func() K8sLabelRetriever {
				return new(mockLabelCache)
			},
			client: func() OceanAWSClusterCostsClient {
				input := clusterCostInput("nonexistent")

				mockClient := new(mockOceanAWSClusterCostsClient)

				mockClient.On("GetClusterAggregatedCosts", mock.Anything, input).Return(nil, errors.New("nonexistent"))
				return mockClient
			},
			clusters: oceanClusters("nonexistent"),
		},
		{
			name: "one cluster",
			labelCache: func() K8sLabelRetriever {
				mockClient := new(mockLabelCache)
				labels := map[string]string{
					"eggs":    "1.75",
					"bacon":   "3.22",
					"sausage": "1.89",
				}
				mockClient.On("GetLabelFor", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(labels, nil)

				return mockClient
			},
			client: func() OceanAWSClusterCostsClient {
				input := clusterCostInput("foo")
				output := clusterCostOutput()
				mockClient := new(mockOceanAWSClusterCostsClient)
				mockClient.On("GetClusterAggregatedCosts", mock.Anything, input).Return(output, nil)
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
		/*
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
					}, */
	}

	logger := zapr.NewLogger(zap.NewNop())

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			collector := NewOceanAWSClusterCostsCollector(ctx, logger, testCase.client(), testCase.clusters, testCase.labelMappings, testCase.labelCache())

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

func clusterCostInput(clusterID string) *aws.ClusterAggregatedCostInput {
	now := time.Now()
	firstDayOfCurrentMonth := now.AddDate(0, 0, -now.Day()+1)
	firstDayOfNextMonth := now.AddDate(0, 1, -now.Day()+1)
	groupByProp := "resource.label.app.kubernetes.io/name"

	return &aws.ClusterAggregatedCostInput{
		OceanId:   spotinst.String(clusterID),
		StartTime: spotinst.String(firstDayOfCurrentMonth.Format("2006-01-02")),
		EndTime:   spotinst.String(firstDayOfNextMonth.Format("2006-01-02")),
		GroupBy:   &groupByProp,
	}
}

func clusterCostOutput() *aws.ClusterAggregatedCostOutput {
	asset, _ := os.Open("testdata/response.json")

	var cunt aws.ClusterAggregatedCostOutput

	decoder := json.NewDecoder(asset)

	decoder.Decode(&cunt)

	//res2B, _ := json.Marshal(cunt)
	//fmt.Println(string(res2B))

	return &cunt
}
