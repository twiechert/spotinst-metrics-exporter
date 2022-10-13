// Package collectors contains Prometheus collectors for Spotinst metrics.
package collectors

import "github.com/prometheus/client_golang/prometheus"

func collectGaugeValue(
	ch chan<- prometheus.Metric,
	desc *prometheus.Desc,
	value float64,
	labelValues []string,
) {
	ch <- prometheus.MustNewConstMetric(
		desc,
		prometheus.GaugeValue,
		value,
		labelValues...,
	)
}
