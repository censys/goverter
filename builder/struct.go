package builder

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/jmattheis/goverter/config"
	"github.com/jmattheis/goverter/method"
	"github.com/jmattheis/goverter/xtype"
)

// Struct handles struct types.
type Struct struct{}

// Matches returns true, if the builder can create handle the given types.
func (*Struct) Matches(_ *MethodContext, source, target *xtype.Type) bool {
	return source.Struct && target.Struct
}

// Build creates conversion source code for the given source and target type.
func (s *Struct) Build(gen Generator, ctx *MethodContext, sourceID *xtype.JenID, source, target *xtype.Type, errPath ErrorPath) ([]jen.Code, *xtype.JenID, *Error) {
	// Optimization for golang sets
	if !source.Named && !target.Named && source.StructType.NumFields() == 0 && target.StructType.NumFields() == 0 {
		return nil, sourceID, nil
	}
	return BuildByAssign(s, gen, ctx, sourceID, source, target, errPath)
}

func (s *Struct) Assign(gen Generator, ctx *MethodContext, assignTo *AssignTo, sourceID *xtype.JenID, source, target *xtype.Type, errPath ErrorPath) ([]jen.Code, *Error) {
	additionalFieldSources, err := parseAutoMap(ctx, source)
	if err != nil {
		return nil, err
	}

	stmt := []jen.Code{}

	definedFields := ctx.DefinedFields(target)
	// assignedOutput tracks the output names already covered by an auto-resolved
	// member, so a field and a setter that cover the same output name don't both get
	// assigned (collision handling for "struct:assign field method").
	assignedOutput := map[string]bool{}
	// explicitOutput holds the output names claimed by members that carry an
	// explicit goverter:map. Such a map always wins, so a competing auto-matched
	// member sharing that output name is skipped rather than also assigned — e.g. a
	// goverter:map naming the setter SetEmail suppresses the field Email, and vice
	// versa.
	explicitOutput := explicitOutputNames(ctx, target)
	usedSourceID := false
	// objectsToMap yields both kinds of target member as a types.Object: a field
	// (e.g. "Name string") and a setter method (e.g. "SetName(string)"). The loop
	// handles both uniformly.
	for _, obj := range objectsToMap(ctx, target) {
		memberName := obj.Name()

		// outputName is the name of the output member this obj covers, and is what
		// everything keys on: goverter:map/ignore, coverage, field/setter
		// collision, and the source auto-match. For a field it's the field name; for
		// a setter it's the setter regex/template result (default $1 == the first
		// capture, e.g. SetEmail -> Email). The matching source value is then found
		// by output name, with source getters normalized via struct:assign:source.
		outputName := memberName
		targetFieldType := xtype.TypeOf(obj.Type())
		setter := false
		setterReturnsErr := false

		var sig *types.Signature
		setterRecognized := false
		if fn, ok := obj.(*types.Func); ok {
			sig, _ = fn.Type().(*types.Signature)
			if sig != nil {
				outputName, setterRecognized = setterName(ctx, memberName)
			}
		}

		// Resolve the goverter:map/ignore addressing this member. It is keyed by the
		// member's own name — the field name for a field, the method name (SetValues)
		// for a setter — so an explicit map/ignore references a setter directly, just
		// like it references a field.
		delete(definedFields, memberName)
		fieldMapping := ctx.Field(target, memberName)

		if fieldMapping.Ignore {
			continue
		}
		if !obj.Exported() && ctx.Conf.IgnoreUnexported {
			continue
		}

		// explicit means the member is addressed by an explicit goverter:map, which
		// always wins over automatic matching and bypasses collision handling.
		explicit := fieldMapping.Source != "" || fieldMapping.Function != nil

		if _, ok := obj.(*types.Func); ok {
			if !setterRecognized && !explicit {
				// not a setter (e.g. String/Validate) and not explicitly mapped
				continue
			}
			paramType, returnsErr, sErr := setterSignature(memberName, sig)
			if sErr != nil {
				return nil, sErr
			}
			targetFieldType = paramType
			setter = true
			setterReturnsErr = returnsErr
		}

		if !explicit && explicitOutput[outputName] {
			// an explicit goverter:map on a sibling member already claims this
			// output name; that map wins, so skip this auto-matched member.
			continue
		}

		if !xtype.Accessible(obj, ctx.OutputPackagePath) {
			cause := unexportedStructError(memberName, source.String, target.String)
			return nil, NewError(cause).Lift(&Path{
				Prefix:     ".",
				SourceID:   "???",
				TargetID:   memberName,
				TargetType: obj.Type().String(),
			})
		}

		if !explicit && assignedOutput[outputName] {
			if ctx.Conf.SetterPrefer != "" {
				// a preferred member already consumed this output name
				continue
			}
			return nil, NewError(setterCollisionError(outputName)).Lift(&Path{
				Prefix:     ".",
				TargetID:   memberName,
				TargetType: targetFieldType.String,
			})
		}

		targetFieldPath := errPath.Field(memberName)
		// synthetic var carrying the member name plus the type to convert into
		// (the field type, or the setter's single parameter type).
		targetField := types.NewVar(token.NoPos, nil, memberName, targetFieldType.T)

		if fieldMapping.Function == nil {
			nextID, nextSource, mapStmt, lift, skip, err := mapField(gen, ctx, outputName, targetField, sourceID, source, fieldMapping, additionalFieldSources, targetFieldPath)
			if skip {
				continue
			}
			if err != nil {
				return nil, err
			}
			usedSourceID = true
			stmt = append(stmt, mapStmt...)

			var memberStmt []jen.Code
			if setter {
				buildStmt, valueID, err := gen.Build(ctx, nextID, nextSource, targetFieldType, targetFieldPath)
				if err != nil {
					return nil, err.Lift(lift...)
				}
				callStmt, err := setterCallStmt(gen, ctx, assignTo, memberName, valueID.Code, setterReturnsErr, targetFieldPath)
				if err != nil {
					return nil, err.Lift(lift...)
				}
				memberStmt = append(buildStmt, callStmt...)
			} else {
				fieldStmt, err := gen.Assign(ctx, AssignOf(assignTo.Stmt.Clone().Dot(memberName)), nextID, nextSource, targetFieldType, targetFieldPath)
				if err != nil {
					return nil, err.Lift(lift...)
				}
				memberStmt = fieldStmt
			}

			if shouldCheckAgainstZero(ctx, nextSource, targetFieldType, assignTo.Update, false) {
				memberStmt = []jen.Code{jen.If(nextID.Code.Clone().Op("!=").Add(xtype.ZeroValue(nextSource.T))).Block(memberStmt...)}
			}
			stmt = append(stmt, memberStmt...)
		} else {
			def := fieldMapping.Function

			sourceLift := []*Path{}
			var functionCallSourceID *xtype.JenID
			var functionCallSourceType *xtype.Type
			if def.Source != nil {
				usedSourceID = true
				nextID, nextSource, mapStmt, mapLift, _, err := mapField(gen, ctx, outputName, targetField, sourceID, source, fieldMapping, additionalFieldSources, targetFieldPath)
				if err != nil {
					return nil, err
				}
				sourceLift = mapLift
				stmt = append(stmt, mapStmt...)

				if fieldMapping.Source == "." && sourceID.ParentPointer != nil &&
					def.Source.AssignableTo(source.AsPointer()) {
					functionCallSourceID = sourceID.ParentPointer
					functionCallSourceType = source.AsPointer()
				} else {
					functionCallSourceID = nextID
					functionCallSourceType = nextSource
				}
			} else {
				sourceLift = append(sourceLift, &Path{
					Prefix:     ".",
					TargetID:   memberName,
					TargetType: targetFieldType.String,
				})
			}

			callStmt, callReturnID, err := gen.CallMethod(ctx, fieldMapping.Function, functionCallSourceID, functionCallSourceType, targetFieldType, targetFieldPath)
			if err != nil {
				return nil, err.Lift(sourceLift...)
			}
			if setter {
				sc, err := setterCallStmt(gen, ctx, assignTo, memberName, callReturnID.Code, setterReturnsErr, targetFieldPath)
				if err != nil {
					return nil, err.Lift(sourceLift...)
				}
				callStmt = append(callStmt, sc...)
			} else {
				callStmt = append(callStmt, assignTo.Stmt.Clone().Dot(memberName).Op("=").Add(callReturnID.Code))
			}

			if shouldCheckAgainstZero(ctx, functionCallSourceType, targetFieldType, assignTo.Update, true) {
				callStmt = []jen.Code{jen.If(functionCallSourceID.Code.Clone().Op("!=").Add(xtype.ZeroValue(functionCallSourceType.T))).Block(callStmt...)}
			}
			stmt = append(stmt, callStmt...)
		}

		if !explicit {
			assignedOutput[outputName] = true
		}
	}
	if !usedSourceID {
		stmt = append(stmt, jen.Id("_").Op("=").Add(sourceID.Code.Clone()))
	}

	for name := range definedFields {
		return nil, NewError(fmt.Sprintf("Field %q does not exist.\nRemove or adjust field settings referencing this field.", name)).Lift(&Path{
			Prefix:     ".",
			TargetID:   name,
			TargetType: "???",
		})
	}

	return stmt, nil
}

// objectsToMap returns the target members that must be satisfied from the source:
// struct fields (when goverter:struct:assign includes "field") and/or setter
// methods (when it includes "method"). types.Var (field) and types.Func (method)
// both implement types.Object, so the assignment loop handles them uniformly.
func objectsToMap(ctx *MethodContext, target *xtype.Type) []types.Object {
	var fields, setters []types.Object
	if ctx.Conf.AssignFields {
		for i := 0; i < target.StructType.NumFields(); i++ {
			fields = append(fields, target.StructType.Field(i))
		}
	}
	if ctx.Conf.AssignSetters && target.Named {
		// setters have pointer receivers; the pointer method set also includes
		// promoted methods from embedded structs.
		ms := types.NewMethodSet(types.NewPointer(target.NamedType))
		for i := 0; i < ms.Len(); i++ {
			if fn, ok := ms.At(i).Obj().(*types.Func); ok {
				setters = append(setters, fn)
			}
		}
	}
	if ctx.Conf.SetterPrefer == "method" {
		return append(setters, fields...)
	}
	return append(fields, setters...)
}

// setterName derives, from a candidate setter method name, the output field name
// it covers (via the setter regex/template, default "$1" == the first capture, so
// SetEmail -> Email). recognized is false when the name doesn't fully match the
// setter regex or the template expands to empty, meaning the method is not a setter
// (e.g. String/Validate); the output name then falls back to the method name.
func setterName(ctx *MethodContext, name string) (outputName string, recognized bool) {
	idx := ctx.Conf.SetterRegex.FindStringSubmatchIndex(name)
	if idx == nil || idx[0] != 0 || idx[1] != len(name) || len(idx) < 4 {
		return name, false
	}
	outputName = string(ctx.Conf.SetterRegex.ExpandString(nil, ctx.Conf.SetterTemplate, name, idx))
	if outputName == "" {
		return name, false
	}
	return outputName, true
}

// sourceNameNormalizer builds the source-side name normalizer from
// struct:assign:source: it rewrites an accessor method name (e.g. a getter) into
// the output field name it provides via the configured regex/template, mirroring
// setterName on the source side. It returns nil when struct:assign:source is not
// configured, leaving source matching exact-name only.
func sourceNameNormalizer(ctx *MethodContext) xtype.NameNormalizer {
	regex := ctx.Conf.SourceRegex
	if regex == nil {
		return nil
	}
	template := ctx.Conf.SourceTemplate
	return func(methodName string) (string, bool) {
		idx := regex.FindStringSubmatchIndex(methodName)
		if idx == nil || idx[0] != 0 || idx[1] != len(methodName) || len(idx) < 4 {
			return "", false
		}
		normalized := string(regex.ExpandString(nil, template, methodName, idx))
		if normalized == "" {
			return "", false
		}
		return normalized, true
	}
}

// explicitOutputNames collects the output names of target members that carry an
// explicit goverter:map (a source path or a conversion function), keyed by each
// member's own name. The assignment loop skips any auto-matched member whose
// output name is claimed here, so an explicit map on one member suppresses a
// competing sibling covering the same output name (e.g. a map naming the setter
// SetEmail suppresses the field Email, and vice versa), letting the map win.
func explicitOutputNames(ctx *MethodContext, target *xtype.Type) map[string]bool {
	explicit := map[string]bool{}
	for _, obj := range objectsToMap(ctx, target) {
		memberName := obj.Name()
		fieldMapping := ctx.Field(target, memberName)
		if fieldMapping.Source == "" && fieldMapping.Function == nil {
			continue
		}
		outputName := memberName
		if _, ok := obj.(*types.Func); ok {
			outputName, _ = setterName(ctx, memberName)
		}
		explicit[outputName] = true
	}
	return explicit
}

// setterSignature validates that a candidate setter method has exactly one
// parameter and returns nothing or a single error. It returns the parameter type
// (the type a source value must be converted into) and whether it returns an error.
func setterSignature(name string, sig *types.Signature) (*xtype.Type, bool, *Error) {
	liftErr := func(cause string) *Error {
		return NewError(cause).Lift(&Path{
			Prefix:     ".",
			SourceID:   "???",
			TargetID:   name,
			TargetType: "???",
		})
	}
	if sig == nil || sig.Params().Len() != 1 {
		return nil, false, liftErr(fmt.Sprintf("Setter method %s must have exactly one parameter.", name))
	}
	returnsErr := false
	switch sig.Results().Len() {
	case 0:
	case 1:
		if !isErrorType(sig.Results().At(0).Type()) {
			return nil, false, liftErr(fmt.Sprintf("Setter method %s must return nothing or a single error, but returns %s.", name, sig.Results().At(0).Type()))
		}
		returnsErr = true
	default:
		return nil, false, liftErr(fmt.Sprintf("Setter method %s must return nothing or a single error.", name))
	}
	return xtype.TypeOf(sig.Params().At(0).Type()), returnsErr, nil
}

// setterCallStmt emits the statements that call a setter method with the given
// value. For an error-returning setter it wraps the call in an error check that
// returns via the converter method (which must therefore return an error too).
func setterCallStmt(gen Generator, ctx *MethodContext, assignTo *AssignTo, methodName string, value *jen.Statement, returnsErr bool, errPath ErrorPath) ([]jen.Code, *Error) {
	call := assignTo.Stmt.Clone().Dot(methodName).Call(value.Clone())
	if !returnsErr {
		return []jen.Code{call}, nil
	}
	ret, ok := gen.ReturnError(ctx, errPath, jen.Id("err"))
	if !ok {
		return nil, NewError(fmt.Sprintf("Setter method %s returns error but the conversion method does not return error.", methodName))
	}
	return []jen.Code{jen.If(jen.Id("err").Op(":=").Add(call), jen.Id("err").Op("!=").Nil()).Block(ret)}, nil
}

func setterCollisionError(outputName string) string {
	return fmt.Sprintf(`Multiple target members cover output field %q.

Set goverter:struct:assign:prefer to "field" or "method" to choose which one wins, or disambiguate with an explicit goverter:map or goverter:ignore.`, outputName)
}

func isErrorType(t types.Type) bool {
	named, ok := t.(*types.Named)
	return ok && named.Obj().Name() == "error" && named.Obj().Pkg() == nil
}

func shouldCheckAgainstZero(ctx *MethodContext, s, t *xtype.Type, isUpdate, call bool) bool {
	switch {
	case !ctx.Conf.UpdateTarget && !isUpdate:
		return false
	case s.Struct && ctx.Conf.IgnoreStructZeroValueField:
		return true
	case s.Basic && ctx.Conf.IgnoreBasicZeroValueField:
		return true
	case ctx.Conf.IgnoreNillableZeroValueField:
		if s.Chan || s.Map || s.Func || s.Signature || s.Interface {
			return true
		}
		if call || (ctx.Conf.SkipCopySameType && types.Identical(s.T, t.T)) {
			return (s.List && !s.ListFixed) || s.Pointer
		}
		return false
	default:
		return false
	}
}

func mapField(
	gen Generator,
	ctx *MethodContext,
	implicitName string,
	targetField *types.Var,
	sourceID *xtype.JenID,
	source *xtype.Type,
	mapping *config.FieldMapping,
	additionalFieldSources []xtype.FieldSources,
	errPath ErrorPath,
) (*xtype.JenID, *xtype.Type, []jen.Code, []*Path, bool, *Error) {
	lift := []*Path{}
	pathString := mapping.Source
	if pathString == "." {
		lift = append(lift, &Path{
			Prefix:     ".",
			SourceID:   " ",
			SourceType: "goverter:map . " + targetField.Name(),
			TargetID:   targetField.Name(),
			TargetType: targetField.Type().String(),
		})
		return sourceID, source, nil, lift, false, nil
	}

	var path []string
	if pathString == "" {
		sourceMatch, err := xtype.FindField(implicitName, ctx.Conf.MatchIgnoreCase, source, additionalFieldSources, sourceNameNormalizer(ctx))
		if err != nil {
			cause := fmt.Sprintf("Cannot match the target field with the source entry: %s.", err.Error())
			skip := false
			if ctx.Conf.IgnoreMissing {
				_, skip = err.(*xtype.NoMatchError)
			}
			return nil, nil, nil, nil, skip, NewError(cause).Lift(&Path{
				Prefix:     ".",
				SourceID:   "???",
				TargetID:   targetField.Name(),
				TargetType: targetField.Type().String(),
			})
		}

		path = sourceMatch.Path
	} else {
		path = strings.Split(pathString, ".")
	}

	var condition *jen.Statement

	nextIDCode := sourceID.Code
	nextSource := source

	for i := 0; i < len(path); i++ {
		if nextSource.Pointer {
			addCondition := nextIDCode.Clone().Op("!=").Nil()
			if condition == nil {
				condition = addCondition
			} else {
				condition = condition.Clone().Op("&&").Add(addCondition)
			}
			nextSource = nextSource.PointerInner
		}
		if !nextSource.Struct {
			cause := fmt.Sprintf("Cannot access '%s' on %s.", path[i], nextSource.T)
			return nil, nil, nil, nil, false, NewError(cause).Lift(&Path{
				Prefix:     ".",
				SourceID:   path[i],
				SourceType: "???",
			}).Lift(lift...)
		}
		sourceMatch, err := xtype.FindExactField(nextSource, path[i], sourceNameNormalizer(ctx))
		if err == nil {
			nextSource = sourceMatch.Type
			nextIDCode = nextIDCode.Clone().Dot(sourceMatch.Name)
			liftPath := &Path{
				Prefix:     ".",
				SourceID:   sourceMatch.Name,
				SourceType: nextSource.String,
			}

			if i == len(path)-1 {
				liftPath.TargetID = targetField.Name()
				liftPath.TargetType = targetField.Type().String()
			}
			lift = append(lift, liftPath)
			continue
		}

		cause := fmt.Sprintf("Cannot find the mapped field on the source entry: %s.", err.Error())
		return nil, nil, []jen.Code{}, nil, false, NewError(cause).Lift(&Path{
			Prefix:     ".",
			SourceID:   path[i],
			SourceType: "???",
		}).Lift(lift...)
	}

	returnID := xtype.VariableID(nextIDCode)
	innerStmt := []jen.Code{}
	if nextSource.Func {
		def, err := method.Parse(nextSource.FuncType, &method.ParseOpts{
			Converter:         nil,
			OutputPackagePath: ctx.OutputPackagePath,
			ErrorPrefix:       "Error parsing struct method",
			Params:            method.ParamsNone,
			ContextMatch:      config.StructMethodContextRegex,
			CustomCall:        nextIDCode,
		}, method.EmptyLocalOpts)
		if err != nil {
			return nil, nil, nil, nil, false, NewError(err.Error()).Lift(lift...)
		}

		methodCallInner, callID, callErr := gen.CallMethod(ctx, def, nil, nil, def.Target, errPath)
		if callErr != nil {
			return nil, nil, nil, nil, false, callErr.Lift(lift...)
		}
		innerStmt = methodCallInner
		nextSource = def.Target
		returnID = callID
		lift = append(lift, &Path{
			Prefix:     "(",
			SourceID:   ")",
			SourceType: def.Target.String,
		})
	}

	if condition != nil && !nextSource.Pointer {
		lift[len(lift)-1].SourceType = fmt.Sprintf("*%s (It is a pointer because the nested property in the goverter:map was a pointer)",
			lift[len(lift)-1].SourceType)
	}

	stmt := []jen.Code{}
	if condition != nil {
		pointerNext := nextSource
		if !nextSource.Pointer {
			pointerNext = nextSource.AsPointer()
		}
		tempName := ctx.Name(pointerNext.ID())
		stmt = append(stmt, jen.Var().Id(tempName).Add(pointerNext.TypeAsJen()))

		if nextSource.Pointer {
			innerStmt = append(innerStmt, jen.Id(tempName).Op("=").Add(returnID.Code))
		} else {
			pstmt, pointerID := returnID.Pointer(nextSource, ctx.Name)
			innerStmt = append(innerStmt, pstmt...)
			innerStmt = append(innerStmt, jen.Id(tempName).Op("=").Add(pointerID.Code))
		}

		stmt = append(stmt, jen.If(condition).Block(innerStmt...))
		nextSource = pointerNext
		returnID = xtype.VariableID(jen.Id(tempName))
	} else {
		stmt = append(stmt, innerStmt...)
	}

	return returnID, nextSource, stmt, lift, false, nil
}

func parseAutoMap(ctx *MethodContext, source *xtype.Type) ([]xtype.FieldSources, *Error) {
	fieldSources := []xtype.FieldSources{}
	for _, field := range ctx.Conf.AutoMap {
		innerSource := source
		lift := []*Path{}
		path := strings.Split(field, ".")
		for _, part := range path {
			field, err := xtype.FindExactField(innerSource, part, sourceNameNormalizer(ctx))
			if err != nil {
				return nil, NewError(err.Error()).Lift(&Path{
					Prefix:     ".",
					SourceID:   part,
					SourceType: "goverter:autoMap",
				}).Lift(lift...)
			}
			lift = append(lift, &Path{
				Prefix:     ".",
				SourceID:   field.Name,
				SourceType: field.Type.String,
			})
			innerSource = field.Type

			switch {
			case innerSource.Pointer && innerSource.PointerInner.Struct:
				innerSource = xtype.TypeOf(innerSource.PointerInner.StructType)
			case innerSource.Struct:
				// ok
			default:
				return nil, NewError(fmt.Sprintf("%s is not a struct or struct pointer", part)).Lift(lift...)
			}
		}

		fieldSources = append(fieldSources, xtype.FieldSources{Path: path, Type: innerSource})
	}
	return fieldSources, nil
}

func unexportedStructError(targetField, sourceType, targetType string) string {
	return fmt.Sprintf(`Cannot set value for unexported field "%s".

See https://goverter.jmattheis.de/guide/unexported-field`, targetField)
}
