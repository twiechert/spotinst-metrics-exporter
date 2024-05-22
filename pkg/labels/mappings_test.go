package labels

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMappings(t *testing.T) {
	t.Run("valid input", func(t *testing.T) {
		expectedMappings := Mappings{
			{resourceLabelName: "foo", prometheusLabelName: "foo"},
			{resourceLabelName: "bar", prometheusLabelName: "baz"},
		}

		resourceLabels := map[string]string{
			"bar": "bar-value",
			"qux": "qux-value",
		}

		mappings, err := ParseMappings("foo,bar=baz")
		assert.NoError(t, err)
		assert.Equal(t, expectedMappings, mappings)
		assert.Equal(t, []string{"foo", "baz"}, mappings.LabelNames())
		assert.Equal(t, []string{"", "bar-value"}, mappings.LabelValues(resourceLabels))
		assert.Equal(t, "foo=foo,bar=baz", mappings.String())
	})

	t.Run("invalid input", func(t *testing.T) {
		for _, input := range []string{"", "foo=,bar=baz", "=foo"} {
			_, err := ParseMappings(input)
			assert.Error(t, err)
		}
	})

	t.Run("set", func(t *testing.T) {
		var mappings Mappings

		expectedMappings := Mappings{
			{resourceLabelName: "foo", prometheusLabelName: "foo"},
		}

		assert.NoError(t, mappings.Set("foo"))
		assert.Equal(t, expectedMappings, mappings)

		expectedMappings = Mappings{
			{resourceLabelName: "foo", prometheusLabelName: "foo"},
			{resourceLabelName: "bar", prometheusLabelName: "baz"},
			{resourceLabelName: "baz", prometheusLabelName: "qux"},
		}

		assert.NoError(t, mappings.Set("bar=baz,baz=qux"))
		assert.Equal(t, expectedMappings, mappings)
	})
}
