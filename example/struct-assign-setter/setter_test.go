package example_test

import (
	"testing"

	example "github.com/jmattheis/goverter/example/struct-assign-setter"
	"github.com/jmattheis/goverter/example/struct-assign-setter/generated"
	"github.com/stretchr/testify/require"
)

func TestConverter(t *testing.T) {
	var c example.Converter = &generated.ConverterImpl{}

	actual := c.Convert(example.Input{Name: "jmattheis", Age: 5})

	expected := example.Person{}
	expected.SetName("jmattheis")
	expected.SetAge(5)
	require.Equal(t, expected, actual)
}
