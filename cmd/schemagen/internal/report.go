// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal

import (
	"fmt"
	"go/ast"
	"strings"
)

type Issue struct {
	ComponentDir string `json:"componentDir" yaml:"componentDir"`
	TypeName     string `json:"typeName" yaml:"typeName"`
	FieldName    string `json:"fieldName" yaml:"fieldName"`
	FieldType    string `json:"fieldType" yaml:"fieldType"`
	File         string `json:"file" yaml:"file"`
	Line         int    `json:"line" yaml:"line"`
	Reason       string `json:"reason" yaml:"reason"`
	SuggestedFix string `json:"suggestedFix" yaml:"suggestedFix"`
}

func (p *Parser) recordIssue(field *ast.Field, fieldName string, err error) {
	if field == nil || err == nil {
		return
	}
	var (
		file string
		line int
	)
	if p.fset != nil {
		pos := p.fset.Position(field.Pos())
		file = pos.Filename
		line = pos.Line
	}
	fieldType := ExprString(field.Type, p.fset)

	issue := Issue{
		ComponentDir: p.config.DirPath,
		TypeName:     p.current.typeName,
		FieldName:    fieldName,
		FieldType:    fieldType,
		File:         file,
		Line:         line,
		Reason:       err.Error(),
		SuggestedFix: suggestFix(err.Error(), fieldType),
	}
	p.issues = append(p.issues, issue)
}

func suggestFix(reason, fieldType string) string {
	lower := strings.ToLower(reason)
	switch {
	case strings.Contains(lower, "unrecognized type in selector"):
		return fmt.Sprintf("Add schema for %s and include its repo in allowedRefs, or add a mapping for this type in .schemagen.yaml.", fieldType)
	case strings.Contains(lower, "type") && strings.Contains(lower, "not found"):
		return "Ensure the type is in the parsed package, or add a mapping in .schemagen.yaml, or add a schema and allowedRefs entry."
	case strings.Contains(lower, "unrecognized embedded field type"):
		return "Replace embedded field with a named field, or add a mapping for the embedded type."
	case strings.Contains(lower, "unrecognized field type"):
		return "Use supported types (struct, map, slice, basic) or add a mapping/schema for this type."
	default:
		return "Review the field type; consider adding a mapping or schema reference in .schemagen.yaml."
	}
}
