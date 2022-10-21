package labels

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/prometheus/common/model"
)

// Labels are a collection of custom labels.
type Labels []Label

// NewLabels create a new Labels value from a list of names.
//
// Returns an error if any of the label names cannot be sanitized to a valid
// prometheus label name.
func NewLabels(names ...string) (Labels, error) {
	labels := make([]Label, 0, len(names))

	for _, name := range names {
		name = strings.TrimSpace(name)

		if label, err := NewLabel(name); err != nil {
			return nil, err
		} else {
			labels = append(labels, label)
		}
	}

	return labels, nil
}

// MustNewLabels is like NewLabels but will panic on error.
func MustNewLabels(names ...string) Labels {
	labels, err := NewLabels(names...)
	if err != nil {
		panic(err)
	}
	return labels
}

// Names returns a list of label names.
func (l Labels) Names() []string {
	names := make([]string, len(l))
	for i, label := range l {
		names[i] = label.Name()
	}
	return names
}

// SanitizedNames returns a list of sanitized label names.
func (l Labels) SanitizedNames() []string {
	names := make([]string, len(l))
	for i, label := range l {
		names[i] = label.SanitizedName()
	}
	return names
}

// Label holds a user defined label name and its sanitized form for use as a
// prometheus label.
type Label struct {
	name          string
	sanitizedName string
}

// NewLabel creates a new Label from a name.
//
// Returns an error if the label name cannot be sanitized to a valid prometheus
// label name.
func NewLabel(name string) (Label, error) {
	sanitizedName, err := sanitizeLabelName(name)
	if err != nil {
		return Label{}, err
	}

	label := Label{
		name:          name,
		sanitizedName: sanitizedName,
	}

	return label, nil
}

// Name returns the name used to create the Label value.
func (l Label) Name() string {
	return l.name
}

// SanitizedName returns a name that can be used as a valid prometheus label
// name.
func (l Label) SanitizedName() string {
	return l.sanitizedName
}

// CustomLabels returns custom labels defined via the `SPOTINST_CUSTOM_LABEL_NAMES`
// environment variable.
//
// Returns an error if any of the label names cannot be sanitized to a valid
// prometheus label name.
func CustomLabels() (Labels, error) {
	customLabelNames, envSet := os.LookupEnv("SPOTINST_CUSTOM_LABEL_NAMES")
	if !envSet || len(customLabelNames) == 0 {
		return Labels{}, nil
	}

	return NewLabels(strings.Split(customLabelNames, ",")...)
}

func sanitizeLabelName(name string) (string, error) {
	if len(name) == 0 {
		return "", errors.New("empty label name")
	}

	var sanitized []rune

	for i, b := range name {
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_' || (b >= '0' && b <= '9' && i > 0) {
			sanitized = append(sanitized, b)
		} else {
			sanitized = append(sanitized, '_')
		}
	}

	name = string(sanitized)

	if strings.HasPrefix(name, model.ReservedLabelPrefix) {
		return "", fmt.Errorf("sanitized label name '%s' must not start with '%s'", name, model.ReservedLabelPrefix)
	}

	return name, nil
}
