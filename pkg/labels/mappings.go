package labels

import (
	"errors"
	"strings"
)

var errEmptyLabelName = errors.New("label names must not be empty")

// Mapping defines a mapping between a Kubernetes resource label and a
// Prometheus label.
type Mapping struct {
	resourceLabelName   string
	prometheusLabelName string
}

// Mappings is a list of label mappings.
type Mappings []Mapping

// ParseMappings parses label mappings from an input string.
//
// Returns an error if the input is malformed.
func ParseMappings(input string) (Mappings, error) {
	pairs := strings.Split(input, ",")
	mappings := make(Mappings, 0, len(pairs))

	for _, pair := range pairs {
		labels := strings.SplitN(pair, "=", 2)

		resourceLabel := labels[0]
		prometheusLabel := resourceLabel
		if len(labels) == 2 {
			prometheusLabel = labels[1]
		}

		if resourceLabel == "" || prometheusLabel == "" {
			return nil, errEmptyLabelName
		}

		mappings = append(mappings, Mapping{
			resourceLabelName:   resourceLabel,
			prometheusLabelName: prometheusLabel,
		})
	}

	return mappings, nil
}

// LabelNames returns the names of the Prometheus labels.
func (m Mappings) LabelNames() []string {
	values := make([]string, 0, len(m))

	for _, mapping := range m {
		values = append(values, mapping.prometheusLabelName)
	}

	return values
}

// LabelValues extracts the values for the configured Prometheus labels from
// the provided labels map.
func (m Mappings) LabelValues(labels map[string]string) []string {
	values := make([]string, 0, len(m))

	for _, mapping := range m {
		values = append(values, labels[mapping.resourceLabelName])
	}

	return values
}

// Set implements pflag.Value.
func (m *Mappings) Set(value string) error {
	mappings, err := ParseMappings(value)
	if err != nil {
		return err
	}

	*m = append(*m, mappings...)
	return nil
}

// String implements pflag.Value.
func (m Mappings) String() string {
	var sb strings.Builder

	for i, mapping := range m {
		if i > 0 {
			sb.WriteRune(',')
		}
		sb.WriteString(mapping.resourceLabelName)
		sb.WriteRune('=')
		sb.WriteString(mapping.prometheusLabelName)
	}

	return sb.String()
}

// Type implements pflag.Value.
func (m Mappings) Type() string {
	return "resource-label[=prometheus-label]"
}
