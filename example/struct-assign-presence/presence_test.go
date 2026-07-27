package example_test

import (
	"testing"

	example "github.com/jmattheis/goverter/example/struct-assign-presence"
	"github.com/jmattheis/goverter/example/struct-assign-presence/generated"
	"github.com/stretchr/testify/require"
)

func TestConverter(t *testing.T) {
	var c example.Converter = &generated.ConverterImpl{}

	actual := c.Convert(example.Input{Email: "jmattheis@example.com"})

	expected := example.Contact{}
	expected.SetEmail("jmattheis@example.com")
	require.Equal(t, expected, actual)
}
