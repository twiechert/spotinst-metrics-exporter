package labels

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCustomLabels_None(t *testing.T) {
	labels, err := CustomLabels()
	assert.NoError(t, err)
	assert.Empty(t, labels)
}

func TestCustomLabels_Valid(t *testing.T) {
	t.Setenv("SPOTINST_CUSTOM_LABEL_NAMES", "foo ,bar ,app.kubernetes.io/name")

	expected := Labels{
		{name: "foo", sanitizedName: "foo"},
		{name: "bar", sanitizedName: "bar"},
		{name: "app.kubernetes.io/name", sanitizedName: "app_kubernetes_io_name"},
	}

	labels, err := CustomLabels()
	assert.NoError(t, err)
	assert.Equal(t, expected, labels)
	assert.Equal(t, []string{"foo", "bar", "app.kubernetes.io/name"}, labels.Names())
	assert.Equal(t, []string{"foo", "bar", "app_kubernetes_io_name"}, labels.SanitizedNames())
}

func TestCustomLabels_EmptyLabelName(t *testing.T) {
	t.Setenv("SPOTINST_CUSTOM_LABEL_NAMES", " , bar")

	labels, err := CustomLabels()
	assert.Error(t, err)
	assert.Empty(t, labels)
}

func TestCustomLabels_ReservedPrefix(t *testing.T) {
	t.Setenv("SPOTINST_CUSTOM_LABEL_NAMES", "__name__")

	labels, err := CustomLabels()
	assert.Error(t, err)
	assert.Empty(t, labels)
}
