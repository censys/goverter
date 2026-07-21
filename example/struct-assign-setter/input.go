package example

// Person is a target type that only exposes setters over its unexported fields,
// similar to structs generated for the gRPC/protobuf Opaque API.
type Person struct {
	name string
	age  int
}

func (p *Person) SetName(name string) { p.name = name }
func (p *Person) SetAge(age int)      { p.age = age }

type Input struct {
	Name string
	Age  int
}

// goverter:converter
// goverter:struct:assign method
type Converter interface {
	Convert(source Input) Person
}
