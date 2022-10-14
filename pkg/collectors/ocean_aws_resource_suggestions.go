package collectors

import (
	"context"
	"strings"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spotinst/spotinst-sdk-go/service/ocean/providers/aws"
	"github.com/spotinst/spotinst-sdk-go/spotinst"
)

// OceanAWSResourceSuggestionsClient is the interface for something that can
// list Ocean resource suggestions.
//
// It is implemented by the Spotinst *aws.ServiceOp client.
type OceanAWSResourceSuggestionsClient interface {
	ListOceanResourceSuggestions(
		context.Context,
		*aws.ListOceanResourceSuggestionsInput,
	) (*aws.ListOceanResourceSuggestionsOutput, error)
}

// OceanAWSResourceSuggestionsCollector is a prometheus collector for the
// resource suggestions of Spotinst Ocean clusters on AWS.
type OceanAWSResourceSuggestionsCollector struct {
	ctx                      context.Context
	logger                   logr.Logger
	client                   OceanAWSResourceSuggestionsClient
	clusters                 []*aws.Cluster
	requestedWorkloadCPU     *prometheus.Desc
	suggestedWorkloadCPU     *prometheus.Desc
	requestedWorkloadMemory  *prometheus.Desc
	suggestedWorkloadMemory  *prometheus.Desc
	requestedContainerCPU    *prometheus.Desc
	suggestedContainerCPU    *prometheus.Desc
	requestedContainerMemory *prometheus.Desc
	suggestedContainerMemory *prometheus.Desc
}

// NewOceanAWSResourceSuggestionsCollector creates a new
// OceanAWSResourceSuggestionsCollector for collecting the resource suggestions
// for the provided list of Ocean clusters.
func NewOceanAWSResourceSuggestionsCollector(
	ctx context.Context,
	logger logr.Logger,
	client OceanAWSResourceSuggestionsClient,
	clusters []*aws.Cluster,
) *OceanAWSResourceSuggestionsCollector {
	collector := &OceanAWSResourceSuggestionsCollector{
		ctx:      ctx,
		logger:   logger,
		client:   client,
		clusters: clusters,
		requestedWorkloadCPU: prometheus.NewDesc(
			prometheus.BuildFQName("spotinst", "ocean_aws", "workload_cpu_requested"),
			"The number of actual CPU units requested by a workload",
			[]string{"ocean_id", "ocean_name", "workload", "namespace", "name"},
			nil,
		),
		suggestedWorkloadCPU: prometheus.NewDesc(
			prometheus.BuildFQName("spotinst", "ocean_aws", "workload_cpu_suggested"),
			"The number of CPU units suggested for a workload",
			[]string{"ocean_id", "ocean_name", "workload", "namespace", "name"},
			nil,
		),
		requestedWorkloadMemory: prometheus.NewDesc(
			prometheus.BuildFQName("spotinst", "ocean_aws", "workload_memory_requested"),
			"The number of actual memory units requested by a workload",
			[]string{"ocean_id", "ocean_name", "workload", "namespace", "name"},
			nil,
		),
		suggestedWorkloadMemory: prometheus.NewDesc(
			prometheus.BuildFQName("spotinst", "ocean_aws", "workload_memory_suggested"),
			"The number of memory units suggested for a workload",
			[]string{"ocean_id", "ocean_name", "workload", "namespace", "name"},
			nil,
		),
		requestedContainerCPU: prometheus.NewDesc(
			prometheus.BuildFQName("spotinst", "ocean_aws", "workload_container_cpu_requested"),
			"The number of actual CPU units requested by a workload's container",
			[]string{"ocean_id", "ocean_name", "workload", "namespace", "name", "container"},
			nil,
		),
		suggestedContainerCPU: prometheus.NewDesc(
			prometheus.BuildFQName("spotinst", "ocean_aws", "workload_container_cpu_suggested"),
			"The number of CPU units suggested for a workload's container",
			[]string{"ocean_id", "ocean_name", "workload", "namespace", "name", "container"},
			nil,
		),
		requestedContainerMemory: prometheus.NewDesc(
			prometheus.BuildFQName("spotinst", "ocean_aws", "workload_container_memory_requested"),
			"The number of actual memory units requested by a workload's container",
			[]string{"ocean_id", "ocean_name", "workload", "namespace", "name", "container"},
			nil,
		),
		suggestedContainerMemory: prometheus.NewDesc(
			prometheus.BuildFQName("spotinst", "ocean_aws", "workload_container_memory_suggested"),
			"The number of memory units suggested for a workload's container",
			[]string{"ocean_id", "ocean_name", "workload", "namespace", "name", "container"},
			nil,
		),
	}

	return collector
}

// Describe implements the prometheus.Collector interface.
func (c *OceanAWSResourceSuggestionsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.requestedWorkloadCPU
	ch <- c.suggestedWorkloadCPU
	ch <- c.requestedWorkloadMemory
	ch <- c.suggestedWorkloadMemory
	ch <- c.requestedContainerCPU
	ch <- c.suggestedContainerCPU
	ch <- c.requestedContainerMemory
	ch <- c.suggestedContainerMemory
}

// Collect implements the prometheus.Collector interface.
func (c *OceanAWSResourceSuggestionsCollector) Collect(ch chan<- prometheus.Metric) {
	for _, cluster := range c.clusters {
		input := &aws.ListOceanResourceSuggestionsInput{
			OceanID: cluster.ID,
		}

		output, err := c.client.ListOceanResourceSuggestions(c.ctx, input)
		if err != nil {
			clusterID := spotinst.StringValue(cluster.ID)
			c.logger.Error(err, "failed to list resource suggestions", "ocean_id", clusterID)
			continue
		}

		c.collectWorkloadSuggestions(ch, output.Suggestions, cluster)
	}
}

func (c *OceanAWSResourceSuggestionsCollector) collectWorkloadSuggestions(
	ch chan<- prometheus.Metric,
	suggestions []*aws.ResourceSuggestion,
	cluster *aws.Cluster,
) {
	for _, suggestion := range suggestions {
		labelValues := []string{
			spotinst.StringValue(cluster.ID),
			spotinst.StringValue(cluster.Name),
			strings.ToLower(spotinst.StringValue(suggestion.ResourceType)),
			spotinst.StringValue(suggestion.Namespace),
			spotinst.StringValue(suggestion.ResourceName),
		}

		collectGaugeValue(ch, c.requestedWorkloadCPU, spotinst.Float64Value(suggestion.RequestedCPU), labelValues)
		collectGaugeValue(ch, c.suggestedWorkloadCPU, spotinst.Float64Value(suggestion.SuggestedCPU), labelValues)
		collectGaugeValue(ch, c.requestedWorkloadMemory, spotinst.Float64Value(suggestion.RequestedMemory), labelValues)
		collectGaugeValue(ch, c.suggestedWorkloadMemory, spotinst.Float64Value(suggestion.SuggestedMemory), labelValues)

		c.collectContainerSuggestions(ch, suggestion.Containers, labelValues)
	}
}

func (c *OceanAWSResourceSuggestionsCollector) collectContainerSuggestions(
	ch chan<- prometheus.Metric,
	suggestions []*aws.ContainerResourceSuggestion,
	workloadLabelValues []string,
) {
	for _, suggestion := range suggestions {
		labelValues := append(workloadLabelValues, spotinst.StringValue(suggestion.Name))

		collectGaugeValue(ch, c.requestedContainerCPU, spotinst.Float64Value(suggestion.RequestedCPU), labelValues)
		collectGaugeValue(ch, c.suggestedContainerCPU, spotinst.Float64Value(suggestion.SuggestedCPU), labelValues)
		collectGaugeValue(ch, c.requestedContainerMemory, spotinst.Float64Value(suggestion.RequestedMemory), labelValues)
		collectGaugeValue(ch, c.suggestedContainerMemory, spotinst.Float64Value(suggestion.SuggestedMemory), labelValues)
	}
}
