package example

// Contact is a target type that exposes setters over its unexported fields,
// similar to structs generated for the gRPC/protobuf Opaque API.
type Contact struct {
	email string
	phone string
}

func (c *Contact) SetEmail(email string) { c.email = email }
func (c *Contact) SetPhone(phone string) { c.phone = phone }

// Input exposes presence methods (HasEmail/HasPhone) alongside its fields, like
// the accessors generated for proto oneof or optional fields. goverter only
// assigns a member when the matching presence method reports it is set.
type Input struct {
	Email string
	Phone string
}

func (i Input) HasEmail() bool { return i.Email != "" }
func (i Input) HasPhone() bool { return i.Phone != "" }

// goverter:converter
// goverter:struct:assign method
// goverter:struct:assign:presence
type Converter interface {
	Convert(source Input) Contact
}
