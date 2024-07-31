package collectors

import (
	"context"
	"fmt"
	"time"

	"github.com/patrickmn/go-cache"

	"github.com/go-logr/logr"
	"github.com/spotinst/spotinst-sdk-go/service/mcs"
	"github.com/spotinst/spotinst-sdk-go/service/ocean/providers/aws"
	"github.com/spotinst/spotinst-sdk-go/spotinst"
)

// OceanAWSClusterCostsClient is the interface for fetching Ocean cluster costs.
//
// It is implemented by the Spotinst *mcs.ServiceOp client.
type OceanMscAWSClusterCostsClient interface {
	GetClusterCosts(context.Context, *mcs.ClusterCostInput) (*mcs.ClusterCostOutput, error)
}

type K8sLabelRetriever interface {
	GetLabelFor(ctx context.Context,
		resourceType string,
		namespace string,
		cluster string,
		resourceIdentifier string) (map[string]string, error)
	PopulateOnce()
	PopulationLoop()
}

// OceanAWSClusterCostsCollector is a prometheus collector for the cost of
// Spotinst Ocean clusters on AWS.
type K8sOceanLabelRetriever struct {
	ctx            context.Context
	logger         logr.Logger
	client         OceanMscAWSClusterCostsClient
	clusters       []*aws.Cluster
	labelCache     *cache.Cache
	isInitialized  bool
	lookupInterval int32
}

// NewOceanAWSClusterCostsCollector creates a new OceanAWSClusterCostsCollector
// for collecting the costs of the provided list of Ocean clusters.
func NewK8sOceanLabelRetriever(
	ctx context.Context,
	logger logr.Logger,
	client mcs.Service,
	clusters []*aws.Cluster,
) K8sLabelRetriever {
	retriever := &K8sOceanLabelRetriever{
		ctx:           ctx,
		logger:        logger,
		client:        client,
		clusters:      clusters,
		labelCache:    cache.New(60*time.Minute, 10*time.Minute),
		isInitialized: false,
	}

	return retriever
}

func (c *K8sOceanLabelRetriever) PopulateOnce() {
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
		} else {

			for _, clusterCost := range output.ClusterCosts {

				for _, namespace := range clusterCost.Namespaces {

					c.iterateOverResources("Deployment", *namespace.Namespace, *cluster.ID, namespace.Deployments)
					c.iterateOverResources("Job", *namespace.Namespace, *cluster.ID, namespace.Jobs)
					c.iterateOverResources("StatefulSet", *namespace.Namespace, *cluster.ID, namespace.StatefulSets)
					c.iterateOverResources("DaemonSet", *namespace.Namespace, *cluster.ID, namespace.DaemonSets)

					// store namespace resource
					cacheKey := c.cacheKeyViaIdentifier("Namespace", *namespace.Namespace, *cluster.ID, *namespace.Namespace)
					c.labelCache.Set(cacheKey, namespace.Labels, cache.DefaultExpiration)
				}
			}
		}

	}
}

// Collect implements the prometheus.Collector interface.
func (c *K8sOceanLabelRetriever) PopulationLoop() {
	for {
		time.Sleep(time.Duration(c.lookupInterval) * time.Second)
		c.PopulateOnce()
		c.isInitialized = true
	}
}

func (c *K8sOceanLabelRetriever) cacheKey(resourceType string, namespace string, cluster string, deployable *mcs.Resource) string {
	resourceIdentifier := deployable.Labels["app.kubernetes.io/name"]
	return c.cacheKeyViaIdentifier(resourceType, namespace, cluster, resourceIdentifier)
}

func (c *K8sOceanLabelRetriever) cacheKeyViaIdentifier(resourceType string, namespace string, cluster string, resourceIdentifier string) string {
	return fmt.Sprintf("%s:%s:%s:%s", cluster, resourceType, namespace, resourceIdentifier)
}

func (c *K8sOceanLabelRetriever) iterateOverResources(resourceType string, namespace string, cluster string, resources []*mcs.Resource) {

	for _, deployable := range resources {
		cacheKey := c.cacheKey(resourceType, namespace, cluster, deployable)
		c.labelCache.Set(cacheKey, deployable.Labels, cache.DefaultExpiration)
	}

}

func (c *K8sOceanLabelRetriever) GetLabelFor(ctx context.Context, resourceType string, namespace string, cluster string, resourceIdentifier string) (map[string]string, error) {
	cacheKey := c.cacheKeyViaIdentifier(resourceType, namespace, cluster, resourceIdentifier)

	if val, found := c.labelCache.Get(cacheKey); found {
		return val.(map[string]string), nil
	} else {
		return nil, fmt.Errorf("expected cache contain entry for key: %s", cacheKey)
	}

}
