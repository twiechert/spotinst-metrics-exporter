package collectors

import (
	"context"
	"time"

	"github.com/Bonial-International-GmbH/spotinst-metrics-exporter/pkg/labels"
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spotinst/spotinst-sdk-go/service/ocean/providers/aws"
	"github.com/spotinst/spotinst-sdk-go/spotinst"
)

// OceanAWSClusterCostsClient is the interface for fetching Ocean cluster costs.
//
// It is implemented by the Spotinst *mcs.ServiceOp client.
type OceanAWSClusterCostsClient interface {
	GetClusterAggregatedCosts(context.Context, *aws.ClusterAggregatedCostInput) (*aws.ClusterAggregatedCostOutput, error)
}

// OceanAWSClusterCostsCollector is a prometheus collector for the cost of
// Spotinst Ocean clusters on AWS.
type OceanAWSClusterCostsCollector struct {
	ctx            context.Context
	logger         logr.Logger
	client         OceanAWSClusterCostsClient
	clusters       []*aws.Cluster
	labelMappings  labels.Mappings
	clusterCost    *prometheus.Desc
	namespaceCost  *prometheus.Desc
	workloadCost   *prometheus.Desc
	resourceCost   *prometheus.Desc
	labelRetriever K8sLabelRetriever
	groupByProp    string
}

// NewOceanAWSClusterCostsCollector creates a new OceanAWSClusterCostsCollector
// for collecting the costs of the provided list of Ocean clusters.
func NewOceanAWSClusterCostsCollector(
	ctx context.Context,
	logger logr.Logger,
	client OceanAWSClusterCostsClient,
	clusters []*aws.Cluster,
	labelMappings labels.Mappings,
	labelRetriever K8sLabelRetriever,
	groupByProp string,
) *OceanAWSClusterCostsCollector {
	collector := &OceanAWSClusterCostsCollector{
		ctx:           ctx,
		logger:        logger,
		client:        client,
		clusters:      clusters,
		labelMappings: labelMappings,
		clusterCost: prometheus.NewDesc(
			prometheus.BuildFQName("spotinst", "ocean_aws_v2", "cluster_cost"),
			"Total cost of an ocean cluster",
			[]string{"ocean_id", "ocean_name"},
			nil,
		),
		namespaceCost: prometheus.NewDesc(
			prometheus.BuildFQName("spotinst", "ocean_aws_v2", "namespace_cost"),
			"Total cost of a namespace",
			append([]string{"ocean_id", "ocean_name", "namespace"}, labelMappings.LabelNames()...),
			nil,
		),
		workloadCost: prometheus.NewDesc(
			prometheus.BuildFQName("spotinst", "ocean_aws_v2", "workload_cost"),
			"Total cost of a workload",
			append([]string{"ocean_id", "ocean_name", "namespace", "name", "workload"}, labelMappings.LabelNames()...),
			nil,
		),

		resourceCost: prometheus.NewDesc(
			prometheus.BuildFQName("spotinst", "ocean_aws_v2", "workload_resource_cost"),
			"Total cost for the given resource of a workload",
			append([]string{"ocean_id", "ocean_name", "namespace", "name", "workload", "resource"}, labelMappings.LabelNames()...),
			nil,
		),
		labelRetriever: labelRetriever,
		groupByProp:    groupByProp,
	}

	return collector
}

// Describe implements the prometheus.Collector interface.
func (c *OceanAWSClusterCostsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.clusterCost
	ch <- c.namespaceCost
	ch <- c.workloadCost
	ch <- c.resourceCost
}

// Collect implements the prometheus.Collector interface.
func (c *OceanAWSClusterCostsCollector) Collect(ch chan<- prometheus.Metric) {
	c.logger.Info("starting collection loop")

	now := time.Now()
	firstDayOfCurrentMonth := now.AddDate(0, 0, -now.Day()+1)
	firstDayOfNextMonth := now.AddDate(0, 1, -now.Day()+1)

	fromDate := spotinst.String(firstDayOfCurrentMonth.Format("2006-01-02"))
	toDate := spotinst.String(firstDayOfNextMonth.Format("2006-01-02"))

	for _, cluster := range c.clusters {
		clusterID := spotinst.StringValue(cluster.ID)
		c.logger.Info("fecthing info for cluster", "ocean_id", clusterID)

		// https://github.com/spotinst/spotinst-sdk-go/blob/9164e3f1eb2050c6a27f631eb0c55ea5fb223917/service/ocean/providers/aws/cluster.go#L1117C41-L1117C48  OceanId == ClusterId
		input := &aws.ClusterAggregatedCostInput{
			StartTime: fromDate,
			EndTime:   toDate,
			GroupBy:   &c.groupByProp,
			OceanId:   cluster.ID,
		}

		output, err := c.client.GetClusterAggregatedCosts(c.ctx, input)
		if err != nil {
			c.logger.Error(err, "failed to fetch cluster costs", "ocean_id", clusterID)
			continue
		}
		// the aggregation yields exactly one result. As a safetety guard, we can check additionally if there is a result at all
		c.collectClusterCosts(ch, output.AggregatedClusterCosts[0], cluster)
	}
}

func (c *OceanAWSClusterCostsCollector) collectClusterCosts(
	ch chan<- prometheus.Metric,
	aggregatedClusterCost *aws.AggregatedClusterCost,
	cluster *aws.Cluster,
) {
	clusterId := spotinst.StringValue(cluster.ID)
	clusterLabelValues := []string{clusterId, spotinst.StringValue(cluster.Name)}

	if aggregatedClusterCost.Result.TotalForDuration != nil {

		collectGaugeValue(ch, c.clusterCost, spotinst.Float64Value(aggregatedClusterCost.Result.TotalForDuration.Summary.Total), clusterLabelValues)
		// since we aggregate over workload and not
		aggregatedNamespaceCost := make(map[string]float64)
		for _, aggregation := range aggregatedClusterCost.Result.TotalForDuration.DetailedCosts.Aggregations {

			// usually there is only one workload per workload name, unless the same workload exists in multiple namespaces
			for _, resource := range aggregation.Resources {

				if *resource.MetaData.Name != "UnusedStorage" {
					namespace, workloadCost := c.collectWorkloadCosts(ch, resource, clusterId, clusterLabelValues)

					if currentNamespaceCost, exists := aggregatedNamespaceCost[namespace]; exists {
						aggregatedNamespaceCost[namespace] = currentNamespaceCost + workloadCost
					} else {
						aggregatedNamespaceCost[namespace] = workloadCost
					}
				}

			}
		}

		for namespace, namespaceCost := range aggregatedNamespaceCost {
			labels, err := c.labelRetriever.GetLabelFor(c.ctx, "Namspace", namespace, clusterId, namespace)
			if err != nil {
				c.logger.Error(err, "failed to fetch namespace labels from spotinst api")
			} else {
				namespaceLabelValues := append(clusterLabelValues, namespace)
				namespaceLabelValues = append(namespaceLabelValues, c.labelMappings.LabelValues(labels)...)
				collectGaugeValue(ch, c.namespaceCost, namespaceCost, namespaceLabelValues)
			}
		}
	}

}

func (c *OceanAWSClusterCostsCollector) collectWorkloadCosts(
	ch chan<- prometheus.Metric,
	workload aws.AggregatedCostResource,
	clusterId string,
	namespaceLabelValues []string,
) (string, float64) {
	workloadType := workload.MetaData.Type
	workloadName := workload.MetaData.Name
	workloadNamespace := workload.MetaData.Namespace

	workloadTotalCost := spotinst.Float64Value(workload.Total)
	workloadStorageCost := spotinst.Float64Value(workload.Storage.Total)
	workloadComputeCost := spotinst.Float64Value(workload.Compute.Total)

	workloadNetworkCost := workloadTotalCost - workloadStorageCost - workloadComputeCost

	labelValues := append(namespaceLabelValues, spotinst.StringValue(workloadNamespace), spotinst.StringValue(workloadName), *workloadType)
	workloadLabels, err := c.labelRetriever.GetLabelFor(c.ctx, *workloadType, *workloadNamespace, clusterId, *workloadName)

	if err != nil {
		c.logger.Error(err, "failed to fetch workload labels from label provider")

	} else {
		labelValues = append(labelValues, c.labelMappings.LabelValues(workloadLabels)...)
		collectGaugeValue(ch, c.workloadCost, spotinst.Float64Value(workload.Total), labelValues)
		networkLabelValues := append(labelValues, "network")
		collectGaugeValue(ch, c.resourceCost, workloadNetworkCost, networkLabelValues)

		storageLabelValues := append(labelValues, "storage")
		collectGaugeValue(ch, c.resourceCost, workloadStorageCost, storageLabelValues)

		computeLabelValues := append(labelValues, "compute")
		collectGaugeValue(ch, c.resourceCost, workloadComputeCost, computeLabelValues)

	}
	return *workloadNamespace, workloadTotalCost
}
