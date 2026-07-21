package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/jmattheis/goverter/config/parse"
	"github.com/jmattheis/goverter/enum"
)

type Common struct {
	FieldSettings                      []string
	WrapErrors                         bool
	WrapErrorsUsing                    string
	IgnoreUnexported                   bool
	IgnoreBasicZeroValueField          bool
	IgnoreStructZeroValueField         bool
	IgnoreNillableZeroValueField       bool
	MatchIgnoreCase                    bool
	IgnoreMissing                      bool
	SkipCopySameType                   bool
	UseZeroValueOnPointerInconsistency bool
	UseUnderlyingTypeMethods           bool
	DefaultUpdate                      bool
	ArgContextRegex                    *regexp.Regexp
	Enum                               enum.Config
	AssignFields                       bool
	AssignSetters                      bool
	SetterRegex                        *regexp.Regexp
	SetterTemplate                     string
	SourceRegex                        *regexp.Regexp
	SourceTemplate                     string
	SetterPrefer                       string
}

func parseCommon(c *Common, cmd, rest string) (fieldSetting bool, err error) {
	switch cmd {
	case "wrapErrors":
		if c.WrapErrorsUsing != "" {
			return false, fmt.Errorf("cannot be used in combination with wrapErrorsUsing")
		}
		c.WrapErrors, err = parse.Bool(rest)
	case "wrapErrorsUsing":
		if c.WrapErrors {
			return false, fmt.Errorf("cannot be used in combination with wrapErrors")
		}
		c.WrapErrorsUsing, err = parse.String(rest)
	case "ignoreUnexported":
		fieldSetting = true
		c.IgnoreUnexported, err = parse.Bool(rest)
	case "update:ignoreZeroValueField":
		fieldSetting = true
		c.IgnoreBasicZeroValueField, err = parse.Bool(rest)
		c.IgnoreStructZeroValueField = c.IgnoreBasicZeroValueField
		c.IgnoreNillableZeroValueField = c.IgnoreBasicZeroValueField
	case "update:ignoreZeroValueField:basic":
		c.IgnoreBasicZeroValueField, err = parse.Bool(rest)
	case "update:ignoreZeroValueField:struct":
		c.IgnoreStructZeroValueField, err = parse.Bool(rest)
	case "update:ignoreZeroValueField:nillable":
		c.IgnoreNillableZeroValueField, err = parse.Bool(rest)
	case "default:update":
		c.DefaultUpdate, err = parse.Bool(rest)
	case "matchIgnoreCase":
		fieldSetting = true
		c.MatchIgnoreCase, err = parse.Bool(rest)
	case "ignoreMissing":
		fieldSetting = true
		c.IgnoreMissing, err = parse.Bool(rest)
	case "skipCopySameType":
		c.SkipCopySameType, err = parse.Bool(rest)
	case "useZeroValueOnPointerInconsistency":
		c.UseZeroValueOnPointerInconsistency, err = parse.Bool(rest)
	case "useUnderlyingTypeMethods":
		c.UseUnderlyingTypeMethods, err = parse.Bool(rest)
	case "enum":
		c.Enum.Enabled, err = parse.Bool(rest)
	case "arg:context:regex":
		c.ArgContextRegex, err = parse.Regex(rest)
	case "enum:unknown":
		c.Enum.Unknown, err = parse.String(rest)
		if err == nil && IsEnumAction(c.Enum.Unknown) {
			err = validateEnumAction(c.Enum.Unknown)
		}
	case "struct:assign":
		fieldSetting = true
		c.AssignFields, c.AssignSetters, err = parseAssign(rest)
	case "struct:assign:setter":
		fieldSetting = true
		c.SetterRegex, c.SetterTemplate, err = parseSetter(rest)
	case "struct:assign:source":
		fieldSetting = true
		c.SourceRegex, c.SourceTemplate, err = parseSource(rest)
	case "struct:assign:prefer":
		fieldSetting = true
		c.SetterPrefer, err = parse.Enum(false, rest, "field", "method")
	case "":
		err = fmt.Errorf("missing setting key")
	default:
		err = fmt.Errorf("unknown setting: %s", cmd)
	}

	return fieldSetting, err
}

// parseAssign parses the "struct:assign" value, which is a space separated list
// containing "field" and/or "method". It returns whether target fields and/or
// setter methods should be part of the assignment coverage.
func parseAssign(rest string) (assignFields, assignSetters bool, err error) {
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return false, false, fmt.Errorf("must have at least one value: field, method")
	}
	for _, f := range fields {
		switch f {
		case "field":
			if assignFields {
				return false, false, fmt.Errorf("duplicate value: field")
			}
			assignFields = true
		case "method":
			if assignSetters {
				return false, false, fmt.Errorf("duplicate value: method")
			}
			assignSetters = true
		default:
			return false, false, fmt.Errorf("invalid value: '%s' must be one of: field, method", f)
		}
	}
	return assignFields, assignSetters, nil
}

// parseSetter parses the "struct:assign:setter" value, which is a regex followed
// by an optional replacement template used to derive the source field name from a
// setter method name. The template defaults to "$1" (the first capture group).
func parseSetter(rest string) (*regexp.Regexp, string, error) {
	parts := strings.SplitN(strings.TrimSpace(rest), " ", 2)
	regex, err := regexp.Compile(parts[0])
	if err != nil {
		return nil, "", err
	}
	if regex.NumSubexp() < 1 {
		return nil, "", fmt.Errorf("setter regex %q must contain at least one capture group to extract the field name", parts[0])
	}
	template := "$1"
	if len(parts) == 2 {
		if trimmed := strings.TrimSpace(parts[1]); trimmed != "" {
			template = trimmed
		}
	}
	return regex, template, nil
}

// parseSource parses the "struct:assign:source" value, a regex followed by an
// optional replacement template used to normalize a source accessor method name
// (e.g. a getter) into the output field name it provides. The template defaults to
// "$1" (the first capture group), so "Get(.*)" makes a source method GetEmail()
// provide the value for output field Email. It is the source-side mirror of
// struct:assign:setter and is generic — any prefix/suffix convention works, not
// just getters.
func parseSource(rest string) (*regexp.Regexp, string, error) {
	parts := strings.SplitN(strings.TrimSpace(rest), " ", 2)
	regex, err := regexp.Compile(parts[0])
	if err != nil {
		return nil, "", err
	}
	if regex.NumSubexp() < 1 {
		return nil, "", fmt.Errorf("source regex %q must contain at least one capture group to extract the field name", parts[0])
	}
	template := "$1"
	if len(parts) == 2 {
		if trimmed := strings.TrimSpace(parts[1]); trimmed != "" {
			template = trimmed
		}
	}
	return regex, template, nil
}
