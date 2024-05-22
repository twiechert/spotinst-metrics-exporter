package collectors

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/Bonial-International-GmbH/spotinst-metrics-exporter/pkg/labels"
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spotinst/spotinst-sdk-go/service/mcs"
	"github.com/spotinst/spotinst-sdk-go/service/ocean/providers/aws"
	"github.com/spotinst/spotinst-sdk-go/spotinst"
)

// OceanAWSClusterCostsClient is the interface for fetching Ocean cluster costs.
//
// It is implemented by the Spotinst *mcs.ServiceOp client.
type OceanAWSClusterCostsClient interface {
	GetClusterCosts(context.Context, *mcs.ClusterCostInput) (*mcs.ClusterCostOutput, error)
}

// OceanAWSClusterCostsCollector is a prometheus collector for the cost of
// Spotinst Ocean clusters on AWS.
type OceanAWSClusterCostsCollector struct {
	ctx           context.Context
	logger        logr.Logger
	client        OceanAWSClusterCostsClient
	clusters      []*aws.Cluster
	labelMappings labels.Mappings
	clusterCost   *prometheus.Desc
	namespaceCost *prometheus.Desc
	workloadCost  *prometheus.Desc
}

// NewOceanAWSClusterCostsCollector creates a new OceanAWSClusterCostsCollector
// for collecting the costs of the provided list of Ocean clusters.
func NewOceanAWSClusterCostsCollector(
	ctx context.Context,
	logger logr.Logger,
	client mcs.Service,
	clusters []*aws.Cluster,
	labelMappings labels.Mappings,
) *OceanAWSClusterCostsCollector {
	collector := &OceanAWSClusterCostsCollector{
		ctx:           ctx,
		logger:        logger,
		client:        client,
		clusters:      clusters,
		labelMappings: labelMappings,
		clusterCost: prometheus.NewDesc(
			prometheus.BuildFQName("spotinst", "ocean_aws", "cluster_cost"),
			"Total cost of an ocean cluster",
			[]string{"ocean_id", "ocean_name"},
			nil,
		),
		namespaceCost: prometheus.NewDesc(
			prometheus.BuildFQName("spotinst", "ocean_aws", "namespace_cost"),
			"Total cost of a namespace",
			append([]string{"ocean_id", "ocean_name", "namespace"}, labelMappings.LabelNames()...),
			nil,
		),
		workloadCost: prometheus.NewDesc(
			prometheus.BuildFQName("spotinst", "ocean_aws", "workload_cost"),
			"Total cost of a workload",
			append([]string{"ocean_id", "ocean_name", "namespace", "name", "workload"}, labelMappings.LabelNames()...),
			nil,
		),
	}

	return collector
}

// Describe implements the prometheus.Collector interface.
func (c *OceanAWSClusterCostsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.clusterCost
	ch <- c.namespaceCost
	ch <- c.workloadCost
}

// Collect implements the prometheus.Collector interface.
func (c *OceanAWSClusterCostsCollector) Collect(ch chan<- prometheus.Metric) {
	now := time.Now()
	firstDayOfCurrentMonth := now.AddDate(0, 0, -now.Day()+1)
	firstDayOfNextMonth := now.AddDate(0, 1, -now.Day()+1)

	fromDate := spotinst.String(firstDayOfCurrentMonth.Format("2006-01-02"))
	toDate := spotinst.String(firstDayOfNextMonth.Format("2006-01-02"))

	for _, cluster := range c.clusters {
		input := &mcs.ClusterCostInput{
			ClusterID: cluster.ControllerClusterID,
			FromDate:  fromDate,
			ToDate:    toDate,
		}

		output, err := c.client.GetClusterCosts(c.ctx, input)
		if err != nil {
			clusterID := spotinst.StringValue(cluster.ID)
			c.logger.Error(err, "failed to fetch cluster costs", "ocean_id", clusterID)
			continue
		}

		c.collectClusterCosts(ch, output.ClusterCosts, cluster)
	}
}

func (c *OceanAWSClusterCostsCollector) collectClusterCosts(
	ch chan<- prometheus.Metric,
	clusters []*mcs.ClusterCost,
	cluster *aws.Cluster,
) {
	labelValues := []string{spotinst.StringValue(cluster.ID), spotinst.StringValue(cluster.Name)}

	for _, cluster := range clusters {
		collectGaugeValue(ch, c.clusterCost, spotinst.Float64Value(cluster.TotalCost), labelValues)

		c.collectNamespaceCosts(ch, cluster.Namespaces, labelValues)
	}
}

func (c *OceanAWSClusterCostsCollector) collectNamespaceCosts(
	ch chan<- prometheus.Metric,
	namespaces []*mcs.Namespace,
	clusterLabelValues []string,
) {
	for _, namespace := range namespaces {
		labelValues := append(clusterLabelValues, spotinst.StringValue(namespace.Namespace))
		namespaceLabelValues := append(labelValues, c.labelMappings.LabelValues(namespace.Labels)...)

		collectGaugeValue(ch, c.namespaceCost, spotinst.Float64Value(namespace.Cost), namespaceLabelValues)

		c.collectWorkloadCosts(ch, namespace.Deployments, "deployment", labelValues)
		c.collectWorkloadCosts(ch, namespace.DaemonSets, "daemonset", labelValues)
		c.collectWorkloadCosts(ch, namespace.StatefulSets, "statefulset", labelValues)
		c.collectWorkloadCosts(ch, namespace.Jobs, "job", labelValues)
	}
}

func (c *OceanAWSClusterCostsCollector) collectWorkloadCosts(
	ch chan<- prometheus.Metric,
	resources []*mcs.Resource,
	workloadName string,
	namespaceLabelValues []string,
) {
	resources = aggregateHighCardinalityResources(resources)

	for _, resource := range resources {
		labelValues := append(namespaceLabelValues, spotinst.StringValue(resource.Name), workloadName)
		labelValues = append(labelValues, c.labelMappings.LabelValues(resource.Labels)...)

		collectGaugeValue(ch, c.workloadCost, spotinst.Float64Value(resource.Cost), labelValues)
	}
}

// Matches timestamps and UUIDs.
var uuidRegex = regexp.MustCompile(`[0-9]{8}|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// AggregateHighCardinalityResources removes timestamps and UUIDs from resource
// names and aggregates the costs for these.
//
// UUIDs and timestamps would cause high metric cardinality and will negatively
// affect performance and storage usage of the metrics engine that will consume
// the metrics. This function is a best-effort to avoid this.
func aggregateHighCardinalityResources(resources []*mcs.Resource) []*mcs.Resource {
	resourceMap := make(map[string]*mcs.Resource, len(resources))

	for _, resource := range resources {
		oldName := spotinst.StringValue(resource.Name)

		name := uuidRegex.ReplaceAllString(oldName, "")

		if name != oldName {
			// Remove hyphens that might be left over after removing the timestamps/UUIDs.
			name = strings.Trim(strings.ReplaceAll(name, "--", "-"), "-")

			// Sum the costs for existing resources.
			if existing, ok := resourceMap[name]; ok {
				resource.Cost = spotinst.Float64(spotinst.Float64Value(resource.Cost) + spotinst.Float64Value(existing.Cost))
			}

			// Update the name.
			resource.Name = spotinst.String(name)
		}

		resourceMap[name] = resource
	}

	cleaned := make([]*mcs.Resource, 0, len(resourceMap))

	for _, resource := range resourceMap {
		cleaned = append(cleaned, resource)
	}

	return cleaned
}
