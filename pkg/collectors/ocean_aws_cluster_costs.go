package collectors

import (
	"context"
	"fmt"
	"time"

	"github.com/Bonial-International-GmbH/spotinst-metrics-exporter/pkg/labels"
	"github.com/go-logr/logr"
	"github.com/patrickmn/go-cache"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spotinst/spotinst-sdk-go/service/ocean/providers/aws"
	"github.com/spotinst/spotinst-sdk-go/spotinst"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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
	ctx           context.Context
	logger        logr.Logger
	client        OceanAWSClusterCostsClient
	clusters      []*aws.Cluster
	labelMappings labels.Mappings
	clusterCost   *prometheus.Desc
	namespaceCost *prometheus.Desc
	workloadCost  *prometheus.Desc
	resourceCost  *prometheus.Desc
	kubeClient    *kubernetes.Clientset
	labelCache    *cache.Cache
}

// NewOceanAWSClusterCostsCollector creates a new OceanAWSClusterCostsCollector
// for collecting the costs of the provided list of Ocean clusters.
func NewOceanAWSClusterCostsCollector(
	ctx context.Context,
	logger logr.Logger,
	client OceanAWSClusterCostsClient,
	kubeClient *kubernetes.Clientset,
	clusters []*aws.Cluster,
	labelMappings labels.Mappings,
) *OceanAWSClusterCostsCollector {
	collector := &OceanAWSClusterCostsCollector{
		ctx:           ctx,
		logger:        logger,
		client:        client,
		kubeClient:    kubeClient,
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

		resourceCost: prometheus.NewDesc(
			prometheus.BuildFQName("spotinst", "ocean_aws", "workload_resource_cost"),
			"Total cost for the given resource of a workload",
			append([]string{"ocean_id", "ocean_name", "namespace", "name", "workload", "resource"}, labelMappings.LabelNames()...),
			nil,
		),
		labelCache: cache.New(60*time.Minute, 10*time.Minute),
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
		groupByProp := "resource.label.app.kubernetes.io/name"
		// https://github.com/spotinst/spotinst-sdk-go/blob/9164e3f1eb2050c6a27f631eb0c55ea5fb223917/service/ocean/providers/aws/cluster.go#L1117C41-L1117C48  OceanId == ClusterId
		input := &aws.ClusterAggregatedCostInput{
			StartTime: fromDate,
			EndTime:   toDate,
			GroupBy:   &groupByProp,
			OceanId:   cluster.ID,
		}

		output, err := c.client.GetClusterAggregatedCosts(c.ctx, input)
		if err != nil {
			clusterID := spotinst.StringValue(cluster.ID)
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
	labelValues := []string{spotinst.StringValue(cluster.ID), spotinst.StringValue(cluster.Name)}

	collectGaugeValue(ch, c.clusterCost, spotinst.Float64Value(aggregatedClusterCost.Result.TotalForDuration.Summary.Total), labelValues)

	// since we aggregate over workload and not
	aggregatedNamespaceCost := make(map[string]float64)
	for _, aggregation := range aggregatedClusterCost.Result.TotalForDuration.DetailedCosts.Aggregations {

		// usually there is only one workload per workload name, unless the same workload exists in multiple namespaces
		for _, resource := range aggregation.Resources {
			namespace, workloadCost := c.collectWorkloadCosts(ch, resource, labelValues)

			if currentNamespaceCost, exists := aggregatedNamespaceCost[namespace]; exists {
				aggregatedNamespaceCost[namespace] = currentNamespaceCost + workloadCost
			} else {
				aggregatedNamespaceCost[namespace] = workloadCost
			}
		}
	}

	for namespace, namespaceCost := range aggregatedNamespaceCost {
		labels, err := c.getLabelsForResource(namespace, namespace, "Namespace")
		if err != nil {
			c.logger.Error(err, "failed to fetch namespace labels from kube api")
		} else {
			namespaceLabelValues := append(labelValues, c.labelMappings.LabelValues(labels)...)
			collectGaugeValue(ch, c.namespaceCost, namespaceCost, namespaceLabelValues)
		}
	}

	//c.collectNamespaceCosts(ch, cluster.Namespaces, labelValues)

}

func (c *OceanAWSClusterCostsCollector) collectWorkloadCosts(
	ch chan<- prometheus.Metric,
	workload aws.AggregatedCostResource,
	namespaceLabelValues []string,
) (string, float64) {
	workloadType := workload.MetaData.Type
	workloadName := workload.MetaData.Name
	workloadNamespace := workload.MetaData.Namespace

	workloadTotalCost := spotinst.Float64Value(workload.Total)
	workloadStorageCost := spotinst.Float64Value(workload.Storage.Total)
	workloadComputeCost := spotinst.Float64Value(workload.Storage.Total)

	workloadNetworkCost := workloadTotalCost - workloadStorageCost - workloadComputeCost

	labelValues := append(namespaceLabelValues, spotinst.StringValue(workloadName), *workloadType)
	workloadLabels, err := c.getLabelsForResource(*workloadName, *workloadNamespace, *workloadType)

	if err != nil {
		c.logger.Error(err, "failed to fetch workload labels from kube api")

	} else {
		labelValues = append(labelValues, c.labelMappings.LabelValues(workloadLabels)...)
		collectGaugeValue(ch, c.workloadCost, spotinst.Float64Value(workload.Total), labelValues)
		resourceLabelValues := append(labelValues, "network")
		collectGaugeValue(ch, c.resourceCost, workloadNetworkCost, resourceLabelValues)

	}
	return *workloadNamespace, workloadTotalCost
}

type LabelProvider interface {
	GetLabels() map[string]string
}

func (c *OceanAWSClusterCostsCollector) getLabelsForResource(resourceName string, namespaceName string, resourceType string) (map[string]string, error) {

	var labelProvider LabelProvider
	var err error
	cacheKey := fmt.Sprintf("%s:%s:%s", resourceType, namespaceName, resourceName)

	labels, found := c.labelCache.Get(cacheKey)
	if found {
		return labels.(map[string]string), nil
	}

	switch resourceType {
	case "Namespace":
		labelProvider, err = c.kubeClient.CoreV1().Namespaces().Get(context.TODO(), resourceName, metav1.GetOptions{})
	case "Deployment":
		labelProvider, err = c.kubeClient.AppsV1().Deployments(namespaceName).Get(context.TODO(), resourceName, metav1.GetOptions{})
	case "StatefulSet":
		labelProvider, err = c.kubeClient.AppsV1().StatefulSets(namespaceName).Get(context.TODO(), resourceName, metav1.GetOptions{})
	case "Job":
		labelProvider, err = c.kubeClient.BatchV1().Jobs(namespaceName).Get(context.TODO(), resourceName, metav1.GetOptions{})
	case "DaemonSet":
		labelProvider, err = c.kubeClient.AppsV1().DaemonSets(namespaceName).Get(context.TODO(), resourceName, metav1.GetOptions{})
	}

	if err == nil {
		c.labelCache.Set(cacheKey, labelProvider.GetLabels(), cache.DefaultExpiration)
		return labelProvider.GetLabels(), nil
	}

	return nil, err

}
