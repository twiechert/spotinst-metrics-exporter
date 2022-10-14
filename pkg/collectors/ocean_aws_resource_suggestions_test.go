package collectors

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/go-logr/zapr"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/spotinst/spotinst-sdk-go/service/ocean/providers/aws"
	"github.com/spotinst/spotinst-sdk-go/spotinst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

type mockOceanAWSResourceSuggestionsClient struct {
	mock.Mock
}

func (m *mockOceanAWSResourceSuggestionsClient) ListOceanResourceSuggestions(
	ctx context.Context,
	input *aws.ListOceanResourceSuggestionsInput,
) (*aws.ListOceanResourceSuggestionsOutput, error) {
	args := m.Called(ctx, input)
	output := args.Get(0)

	if output == nil {
		return nil, args.Error(1)
	}

	return output.(*aws.ListOceanResourceSuggestionsOutput), args.Error(1)
}

func TestOceanAWSResourceSuggestionsCollector(t *testing.T) {
	testCases := []struct {
		name     string
		client   func() OceanAWSResourceSuggestionsClient
		expected string
		clusters []*aws.Cluster
	}{
		{
			name: "no cluster, no output",
			client: func() OceanAWSResourceSuggestionsClient {
				return new(mockOceanAWSResourceSuggestionsClient)
			},
		},
		{
			name: "nonexistent cluster",
			client: func() OceanAWSResourceSuggestionsClient {
				input := resourceSuggestionsInput("nonexistent")

				mockClient := new(mockOceanAWSResourceSuggestionsClient)
				mockClient.On("ListOceanResourceSuggestions", mock.Anything, input).Return(nil, errors.New("nonexistent"))
				return mockClient
			},
			clusters: oceanClusters("nonexistent"),
		},
		{
			name: "one cluster",
			client: func() OceanAWSResourceSuggestionsClient {
				input := resourceSuggestionsInput("foo")
				output := resourceSuggestionsOutput(resourceSuggestion(
					"foo-deployment", "deployment", "foo-ns",
					200, 1000, 100, 2000,
					containerResourceSuggestion("foo-container", 200, 900, 90, 1800),
				))

				mockClient := new(mockOceanAWSResourceSuggestionsClient)
				mockClient.On("ListOceanResourceSuggestions", mock.Anything, input).Return(output, nil)
				return mockClient
			},
			clusters: oceanClusters("foo"),
			expected: `
                # HELP spotinst_ocean_aws_workload_container_cpu_requested The number of actual CPU units requested by a workload's container
                # TYPE spotinst_ocean_aws_workload_container_cpu_requested gauge
                spotinst_ocean_aws_workload_container_cpu_requested{container="foo-container",name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 900
                # HELP spotinst_ocean_aws_workload_container_cpu_suggested The number of CPU units suggested for a workload's container
                # TYPE spotinst_ocean_aws_workload_container_cpu_suggested gauge
                spotinst_ocean_aws_workload_container_cpu_suggested{container="foo-container",name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 200
                # HELP spotinst_ocean_aws_workload_container_memory_requested The number of actual memory units requested by a workload's container
                # TYPE spotinst_ocean_aws_workload_container_memory_requested gauge
                spotinst_ocean_aws_workload_container_memory_requested{container="foo-container",name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 1800
                # HELP spotinst_ocean_aws_workload_container_memory_suggested The number of memory units suggested for a workload's container
                # TYPE spotinst_ocean_aws_workload_container_memory_suggested gauge
                spotinst_ocean_aws_workload_container_memory_suggested{container="foo-container",name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 90
                # HELP spotinst_ocean_aws_workload_cpu_requested The number of actual CPU units requested by a workload
                # TYPE spotinst_ocean_aws_workload_cpu_requested gauge
                spotinst_ocean_aws_workload_cpu_requested{name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 1000
                # HELP spotinst_ocean_aws_workload_cpu_suggested The number of CPU units suggested for a workload
                # TYPE spotinst_ocean_aws_workload_cpu_suggested gauge
                spotinst_ocean_aws_workload_cpu_suggested{name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 200
                # HELP spotinst_ocean_aws_workload_memory_requested The number of actual memory units requested by a workload
                # TYPE spotinst_ocean_aws_workload_memory_requested gauge
                spotinst_ocean_aws_workload_memory_requested{name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 2000
                # HELP spotinst_ocean_aws_workload_memory_suggested The number of memory units suggested for a workload
                # TYPE spotinst_ocean_aws_workload_memory_suggested gauge
                spotinst_ocean_aws_workload_memory_suggested{name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 100
            `,
		},
		{
			name: "one cluster, two resource suggestions",
			client: func() OceanAWSResourceSuggestionsClient {
				mockClient := new(mockOceanAWSResourceSuggestionsClient)

				input := resourceSuggestionsInput("foo")
				output := resourceSuggestionsOutput(
					resourceSuggestion(
						"foo-deployment", "deployment", "foo-ns",
						200, 1000, 100, 2000,
						containerResourceSuggestion("foo-container", 200, 900, 90, 1800),
					),
					resourceSuggestion(
						"bar-daemonset", "daemonSet", "bar-ns",
						199, 999, 99, 1999,
						containerResourceSuggestion("bar-container", 199, 899, 89, 1799),
					),
				)

				mockClient.On("ListOceanResourceSuggestions", mock.Anything, input).Return(output, nil)
				return mockClient
			},
			clusters: oceanClusters("foo"),
			expected: `
                # HELP spotinst_ocean_aws_workload_container_cpu_requested The number of actual CPU units requested by a workload's container
                # TYPE spotinst_ocean_aws_workload_container_cpu_requested gauge
                spotinst_ocean_aws_workload_container_cpu_requested{container="bar-container",name="bar-daemonset",namespace="bar-ns",ocean_id="foo",ocean_name="ocean-foo",workload="daemonset"} 899
                spotinst_ocean_aws_workload_container_cpu_requested{container="foo-container",name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 900
                # HELP spotinst_ocean_aws_workload_container_cpu_suggested The number of CPU units suggested for a workload's container
                # TYPE spotinst_ocean_aws_workload_container_cpu_suggested gauge
                spotinst_ocean_aws_workload_container_cpu_suggested{container="bar-container",name="bar-daemonset",namespace="bar-ns",ocean_id="foo",ocean_name="ocean-foo",workload="daemonset"} 199
                spotinst_ocean_aws_workload_container_cpu_suggested{container="foo-container",name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 200
                # HELP spotinst_ocean_aws_workload_container_memory_requested The number of actual memory units requested by a workload's container
                # TYPE spotinst_ocean_aws_workload_container_memory_requested gauge
                spotinst_ocean_aws_workload_container_memory_requested{container="bar-container",name="bar-daemonset",namespace="bar-ns",ocean_id="foo",ocean_name="ocean-foo",workload="daemonset"} 1799
                spotinst_ocean_aws_workload_container_memory_requested{container="foo-container",name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 1800
                # HELP spotinst_ocean_aws_workload_container_memory_suggested The number of memory units suggested for a workload's container
                # TYPE spotinst_ocean_aws_workload_container_memory_suggested gauge
                spotinst_ocean_aws_workload_container_memory_suggested{container="bar-container",name="bar-daemonset",namespace="bar-ns",ocean_id="foo",ocean_name="ocean-foo",workload="daemonset"} 89
                spotinst_ocean_aws_workload_container_memory_suggested{container="foo-container",name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 90
                # HELP spotinst_ocean_aws_workload_cpu_requested The number of actual CPU units requested by a workload
                # TYPE spotinst_ocean_aws_workload_cpu_requested gauge
                spotinst_ocean_aws_workload_cpu_requested{name="bar-daemonset",namespace="bar-ns",ocean_id="foo",ocean_name="ocean-foo",workload="daemonset"} 999
                spotinst_ocean_aws_workload_cpu_requested{name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 1000
                # HELP spotinst_ocean_aws_workload_cpu_suggested The number of CPU units suggested for a workload
                # TYPE spotinst_ocean_aws_workload_cpu_suggested gauge
                spotinst_ocean_aws_workload_cpu_suggested{name="bar-daemonset",namespace="bar-ns",ocean_id="foo",ocean_name="ocean-foo",workload="daemonset"} 199
                spotinst_ocean_aws_workload_cpu_suggested{name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 200
                # HELP spotinst_ocean_aws_workload_memory_requested The number of actual memory units requested by a workload
                # TYPE spotinst_ocean_aws_workload_memory_requested gauge
                spotinst_ocean_aws_workload_memory_requested{name="bar-daemonset",namespace="bar-ns",ocean_id="foo",ocean_name="ocean-foo",workload="daemonset"} 1999
                spotinst_ocean_aws_workload_memory_requested{name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 2000
                # HELP spotinst_ocean_aws_workload_memory_suggested The number of memory units suggested for a workload
                # TYPE spotinst_ocean_aws_workload_memory_suggested gauge
                spotinst_ocean_aws_workload_memory_suggested{name="bar-daemonset",namespace="bar-ns",ocean_id="foo",ocean_name="ocean-foo",workload="daemonset"} 99
                spotinst_ocean_aws_workload_memory_suggested{name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 100
            `,
		},
		{
			name: "three clusters, one nonexistent",
			client: func() OceanAWSResourceSuggestionsClient {
				mockClient := new(mockOceanAWSResourceSuggestionsClient)

				input := resourceSuggestionsInput("foo")
				output := resourceSuggestionsOutput(resourceSuggestion(
					"foo-deployment", "deployment", "foo-ns",
					200, 1000, 100, 2000,
				))

				mockClient.On("ListOceanResourceSuggestions", mock.Anything, input).Return(output, nil)

				input = resourceSuggestionsInput("nonexistent")
				mockClient.On("ListOceanResourceSuggestions", mock.Anything, input).Return(nil, errors.New("nonexistent"))

				input = resourceSuggestionsInput("bar")
				output = resourceSuggestionsOutput(resourceSuggestion(
					"bar-daemonset", "daemonSet", "bar-ns",
					199, 999, 99, 1999,
				))

				mockClient.On("ListOceanResourceSuggestions", mock.Anything, input).Return(output, nil)
				return mockClient
			},
			clusters: oceanClusters("foo", "nonexistent", "bar"),
			expected: `
                # HELP spotinst_ocean_aws_workload_cpu_requested The number of actual CPU units requested by a workload
                # TYPE spotinst_ocean_aws_workload_cpu_requested gauge
                spotinst_ocean_aws_workload_cpu_requested{name="bar-daemonset",namespace="bar-ns",ocean_id="bar",ocean_name="ocean-bar",workload="daemonset"} 999
                spotinst_ocean_aws_workload_cpu_requested{name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 1000
                # HELP spotinst_ocean_aws_workload_cpu_suggested The number of CPU units suggested for a workload
                # TYPE spotinst_ocean_aws_workload_cpu_suggested gauge
                spotinst_ocean_aws_workload_cpu_suggested{name="bar-daemonset",namespace="bar-ns",ocean_id="bar",ocean_name="ocean-bar",workload="daemonset"} 199
                spotinst_ocean_aws_workload_cpu_suggested{name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 200
                # HELP spotinst_ocean_aws_workload_memory_requested The number of actual memory units requested by a workload
                # TYPE spotinst_ocean_aws_workload_memory_requested gauge
                spotinst_ocean_aws_workload_memory_requested{name="bar-daemonset",namespace="bar-ns",ocean_id="bar",ocean_name="ocean-bar",workload="daemonset"} 1999
                spotinst_ocean_aws_workload_memory_requested{name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 2000
                # HELP spotinst_ocean_aws_workload_memory_suggested The number of memory units suggested for a workload
                # TYPE spotinst_ocean_aws_workload_memory_suggested gauge
                spotinst_ocean_aws_workload_memory_suggested{name="bar-daemonset",namespace="bar-ns",ocean_id="bar",ocean_name="ocean-bar",workload="daemonset"} 99
                spotinst_ocean_aws_workload_memory_suggested{name="foo-deployment",namespace="foo-ns",ocean_id="foo",ocean_name="ocean-foo",workload="deployment"} 100
            `,
		},
	}

	logger := zapr.NewLogger(zap.NewNop())

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			collector := NewOceanAWSResourceSuggestionsCollector(ctx, logger, testCase.client(), testCase.clusters)

			assert.NoError(t, testutil.CollectAndCompare(collector, strings.NewReader(testCase.expected)))
		})
	}
}

func resourceSuggestionsInput(oceanID string) *aws.ListOceanResourceSuggestionsInput {
	return &aws.ListOceanResourceSuggestionsInput{OceanID: spotinst.String(oceanID)}
}

func resourceSuggestionsOutput(suggestions ...*aws.ResourceSuggestion) *aws.ListOceanResourceSuggestionsOutput {
	return &aws.ListOceanResourceSuggestionsOutput{
		Suggestions: suggestions,
	}
}

func resourceSuggestion(
	name, typ, ns string,
	scpu, rcpu, smem, rmem float64,
	containers ...*aws.ContainerResourceSuggestion,
) *aws.ResourceSuggestion {
	return &aws.ResourceSuggestion{
		ResourceName:    spotinst.String(name),
		ResourceType:    spotinst.String(typ),
		Namespace:       spotinst.String(ns),
		SuggestedCPU:    spotinst.Float64(scpu),
		RequestedCPU:    spotinst.Float64(rcpu),
		SuggestedMemory: spotinst.Float64(smem),
		RequestedMemory: spotinst.Float64(rmem),
		Containers:      containers,
	}
}

func containerResourceSuggestion(
	name string,
	scpu, rcpu, smem, rmem float64,
) *aws.ContainerResourceSuggestion {
	return &aws.ContainerResourceSuggestion{
		Name:            spotinst.String(name),
		SuggestedCPU:    spotinst.Float64(scpu),
		RequestedCPU:    spotinst.Float64(rcpu),
		SuggestedMemory: spotinst.Float64(smem),
		RequestedMemory: spotinst.Float64(rmem),
	}
}
