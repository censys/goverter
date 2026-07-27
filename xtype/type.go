package xtype

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/dave/jennifer/jen"
)

// ThisVar is used as name for the reference to the converter interface.
const ThisVar = "c"

// Signature represents a signature for conversion.
type Signature struct {
	Source types.Type
	Target types.Type
}

func (s *Signature) Identical(other Signature) bool {
	return types.Identical(s.Source, other.Source) && types.Identical(s.Target, other.Target)
}

func SignatureOf(source, target *Type) Signature {
	return Signature{Source: source.T, Target: target.T}
}

// Type is a helper wrapper for types.Type.
type Type struct {
	String        string
	T             types.Type
	Interface     bool
	InterfaceType *types.Interface
	Struct        bool
	StructType    *types.Struct
	Named         bool
	NamedType     *types.Named
	Pointer       bool
	PointerType   *types.Pointer
	PointerInner  *Type
	List          bool
	ListFixed     bool
	ListLen       int64
	ListInner     *Type
	Map           bool
	MapType       *types.Map
	MapKey        *Type
	MapValue      *Type
	Basic         bool
	BasicType     *types.Basic
	Signature     bool
	SignatureType *types.Signature
	Func          bool
	FuncType      *types.Func
	Chan          bool
	ChanType      *types.Chan

	enum *Enum
}

func (t *Type) AssignableTo(other *Type) bool {
	return types.AssignableTo(t.T, other.T)
}

func (t *Type) AsPointer() *Type {
	return TypeOf(t.AsPointerType())
}

func (t *Type) AsPointerType() *types.Pointer {
	return types.NewPointer(t.T)
}

func (t *Type) inStruct(source *Type, field string) *Type {
	if t.Signature && source.Named {
		t.FuncType = types.NewFunc(-1, source.NamedType.Obj().Pkg(), field, t.SignatureType)
		t.Func = true
	}

	return t
}

// StructField holds the type of a struct field and its name.
type StructField struct {
	Path []string
	Type *Type
}

type SimpleStructField struct {
	Name string
	Type *Type
}

// StructField returns the type of a struct field and its name upon successful match or
// an error if it is not found. This method will also return a detailed error if matchIgnoreCase
// is enabled and there are multiple non-exact matches.
// NameNormalizer rewrites a source accessor method name into the output field
// name it provides (e.g. GetEmail -> Email), reporting whether the name is
// recognized. It is built from the struct:assign:source setting; a nil normalizer
// means no source-name normalization (exact matching only).
type NameNormalizer func(methodName string) (string, bool)

// findAllFields looks up a source member by output name. It returns the single
// exact-name match (a field or method named exactly name), if any, plus the list
// of inexact matches — matchIgnoreCase hits and struct:assign:source-normalized
// accessors (e.g. getters). An exact match always wins; the inexact list is only
// consulted when there is no exact match, and more than one inexact match is an
// ambiguity the caller must reject.
func (t Type) findAllFields(path []string, name string, ignoreCase bool, normalize NameNormalizer) (*StructField, []*StructField) {
	if !t.Struct {
		panic("trying to get field of non struct")
	}

	var inexactMatches []*StructField
	build := func(obj types.Object) *StructField {
		newPath := append([]string{}, path...)
		newPath = append(newPath, obj.Name())
		return &StructField{Path: newPath, Type: TypeOf(obj.Type()).inStruct(&t, obj.Name())}
	}
	handleField := func(obj types.Object) *StructField {
		exact := obj.Name() == name
		if exact {
			return build(obj)
		}
		if ignoreCase && strings.EqualFold(obj.Name(), name) {
			inexactMatches = append(inexactMatches, build(obj))
		}
		return nil
	}
	handleMethod := func(obj types.Object) *StructField {
		if obj.Name() == name {
			return build(obj)
		}
		if ignoreCase && strings.EqualFold(obj.Name(), name) {
			inexactMatches = append(inexactMatches, build(obj))
		}
		// struct:assign:source recognizes accessor methods (e.g. getters) by
		// normalizing their name to the output field name they provide. Such a
		// match is inexact: an exact field or method name always wins.
		if normalize != nil {
			if normalized, ok := normalize(obj.Name()); ok {
				if normalized == name || (ignoreCase && strings.EqualFold(normalized, name)) {
					inexactMatches = append(inexactMatches, build(obj))
				}
			}
		}
		return nil
	}

	for y := 0; y < t.StructType.NumFields(); y++ {
		if exact := handleField(t.StructType.Field(y)); exact != nil {
			return exact, inexactMatches
		}
	}

	if t.Named {
		// Use the pointer method set rather than NamedType.NumMethods() so that
		// promoted methods from embedded types participate in matching, mirroring
		// how struct:assign:setter and struct:assign:presence resolve methods.
		ms := types.NewMethodSet(types.NewPointer(t.NamedType))
		for y := 0; y < ms.Len(); y++ {
			if exact := handleMethod(ms.At(y).Obj()); exact != nil {
				return exact, inexactMatches
			}
		}
	}

	return nil, inexactMatches
}

type FieldSources struct {
	Path []string
	Type *Type
}

func FindExactField(source *Type, name string, normalize NameNormalizer) (*SimpleStructField, error) {
	exactMatch, fallback := source.findAllFields(nil, name, false, normalize)
	if exactMatch == nil && len(fallback) == 1 {
		// a single struct:assign:source (getter) match satisfies an exact lookup
		exactMatch = fallback[0]
	}
	if exactMatch == nil {
		return nil, fmt.Errorf("%q does not exist", name)
	}
	return &SimpleStructField{Name: exactMatch.Path[0], Type: exactMatch.Type}, nil
}

type NoMatchError struct{ Field string }

func (err *NoMatchError) Error() string {
	return fmt.Sprintf("\"%s\" does not exist", err.Field)
}

func FindField(name string, ignoreCase bool, source *Type, additionalFieldSources []FieldSources, normalize NameNormalizer) (*StructField, error) {
	exactMatch, inexactMatches := source.findAllFields(nil, name, ignoreCase, normalize)
	var exactMatches []*StructField
	if exactMatch != nil {
		exactMatches = append(exactMatches, exactMatch)
	}

	for _, source := range additionalFieldSources {
		sourceExactMatch, sourceInexactMatches := source.Type.findAllFields(source.Path, name, ignoreCase, normalize)
		if sourceExactMatch != nil {
			exactMatches = append(exactMatches, sourceExactMatch)
		}
		inexactMatches = append(inexactMatches, sourceInexactMatches...)
	}

	matches := exactMatches
	if len(matches) == 0 {
		matches = inexactMatches
	}

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return nil, &NoMatchError{Field: name}
	default:
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, strings.Join(m.Path, "."))
		}
		return nil, ambiguousMatchError(name, names)
	}
}

// JenID a jennifer code wrapper with extra infos.
type JenID struct {
	ParentPointer *JenID
	Code          *jen.Statement
	Variable      bool
}

func (j *JenID) Pointer(t *Type, namer func(string) string) ([]jen.Code, *JenID) {
	if j.Variable {
		return nil, OtherID(jen.Op("&").Add(j.Code.Clone()))
	}

	name := namer(t.ID())
	stmt := []jen.Code{jen.Id(name).Op(":=").Add(j.Code.Clone())}
	return stmt, OtherID(jen.Op("&").Id(name))
}

func (j *JenID) Deref(source *Type) *JenID {
	valueSourceID := jen.Op("*").Add(j.Code.Clone())
	if !source.PointerInner.Basic {
		valueSourceID = jen.Parens(valueSourceID)
	}
	innerID := OtherID(valueSourceID)
	innerID.ParentPointer = j
	return innerID
}

// VariableID is used, when the ID can be referenced. F.ex it is not a function call.
func VariableID(code *jen.Statement) *JenID {
	return &JenID{Code: code, Variable: true}
}

// OtherID is used, when the ID isn't a variable id.
func OtherID(code *jen.Statement) *JenID {
	return &JenID{Code: code, Variable: false}
}

// TypeOf creates a Type.
func TypeOf(t types.Type) *Type {
	t = types.Unalias(t)
	rt := &Type{}
	rt.T = t
	rt.String = t.String()
	applyTo(rt, t)
	return rt
}

func applyTo(rt *Type, t types.Type) {
	switch value := t.(type) {
	case *types.Pointer:
		rt.Pointer = true
		rt.PointerType = value
		rt.PointerInner = TypeOf(value.Elem())
	case *types.Basic:
		rt.Basic = true
		rt.BasicType = value
	case *types.Map:
		rt.Map = true
		rt.MapType = value
		rt.MapKey = TypeOf(value.Key())
		rt.MapValue = TypeOf(value.Elem())
	case *types.Slice:
		rt.List = true
		rt.ListInner = TypeOf(value.Elem())
	case *types.Array:
		rt.List = true
		rt.ListFixed = true
		rt.ListInner = TypeOf(value.Elem())
		rt.ListLen = value.Len()
	case *types.Named:
		rt.Named = true
		rt.NamedType = value
		applyTo(rt, value.Underlying())
	case *types.Struct:
		rt.Struct = true
		rt.StructType = value
	case *types.Interface:
		rt.Interface = true
		rt.InterfaceType = value
	case *types.Signature:
		rt.Signature = true
		rt.SignatureType = value
	case *types.Chan:
		rt.Chan = true
		rt.ChanType = value
	case *types.TypeParam:
		// ignore
	default:
		panic("unknown types.Type " + t.String())
	}
}

// ID returns a deteministically generated id that may be used as variable.
func (t *Type) ID() string {
	return t.asID(true, true)
}

// UnescapedID returns a deteministically generated id that may be used as variable
// reserved keywords aren't escaped.
func (t *Type) UnescapedID() string {
	return t.asID(true, false)
}

func (t *Type) asID(seeNamed, escapeReserved bool) string {
	if seeNamed && t.Named {
		pkg := t.NamedType.Obj().Pkg()
		name := t.NamedType.Obj().Name()
		switch {
		case pkg != nil:
			name = pkg.Name() + name
		case escapeReserved:
			name = "x" + name
		}
		return name
	}
	if t.List {
		return t.ListInner.asID(true, false) + "List"
	}
	if t.Basic {
		if escapeReserved {
			return "x" + t.BasicType.String()
		}
		return t.BasicType.String()
	}
	if t.Pointer {
		return "p" + strings.Title(t.PointerInner.asID(true, false))
	}
	if t.Map {
		return "map" + strings.Title(t.MapKey.asID(true, false)+strings.Title(t.MapValue.asID(true, false)))
	}
	if t.Struct {
		return "unnamed"
	}
	if t.Chan {
		return "chan"
	}
	return "unknown"
}

// TypeAsJen returns a jen representation of the type.
func (t Type) TypeAsJen() *jen.Statement {
	if t.Named {
		return toCode(t.NamedType)
	}
	return toCode(t.T)
}

func ambiguousMatchError(name string, ambNames []string) error {
	return fmt.Errorf(`multiple matches found for %q. Possible matches: %s.

Explicitly define the mapping via goverter:map. Example:

    goverter:map %s %s

See https://goverter.jmattheis.de/reference/map`, name, strings.Join(ambNames, ", "), ambNames[0], name)
}
