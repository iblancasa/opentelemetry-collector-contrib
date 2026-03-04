// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal

import (
	"fmt"
	"go/types"

	"github.com/iancoleman/strcase"
	"golang.org/x/tools/go/packages"
)

func (p *Parser) resolveExternalType(pkgPath, typeName string) (SchemaElement, bool, error) {
	pkg, err := p.loadExternalPackage(pkgPath)
	if err != nil {
		return nil, false, err
	}
	if pkg == nil || pkg.Types == nil {
		return nil, false, nil
	}
	obj := pkg.Types.Scope().Lookup(typeName)
	if obj == nil {
		return nil, false, nil
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil, false, nil
	}
	elem, ok := p.schemaFromExternalType(tn.Type())
	if !ok || elem == nil {
		return nil, false, nil
	}
	setCustomType(elem, fmt.Sprintf("%s.%s", pkgPath, strcase.ToSnake(typeName)))
	return elem, true, nil
}

func (p *Parser) loadExternalPackage(pkgPath string) (*packages.Package, error) {
	if pkg, ok := p.externalPkgs[pkgPath]; ok {
		return pkg, nil
	}
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedModule,
		Dir:  p.config.DirPath,
	}, pkgPath)
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 {
		return nil, nil
	}
	pkg := pkgs[0]
	if len(pkg.Errors) > 0 {
		return nil, pkg.Errors[0]
	}
	p.externalPkgs[pkgPath] = pkg
	return pkg, nil
}

func (p *Parser) schemaFromExternalType(t types.Type) (SchemaElement, bool) {
	if t == nil {
		return nil, false
	}
	t = types.Unalias(t)
	switch tt := t.(type) {
	case *types.Named:
		elem, ok := p.schemaFromExternalType(tt.Underlying())
		if !ok || elem == nil {
			return nil, false
		}
		if obj := tt.Obj(); obj != nil && obj.Pkg() != nil {
			setCustomType(elem, fmt.Sprintf("%s.%s", obj.Pkg().Path(), strcase.ToSnake(obj.Name())))
		}
		return elem, true
	case *types.Basic:
		schemaType, ok := schemaTypeFromBasic(tt)
		if !ok {
			return nil, false
		}
		return CreateSimpleField(schemaType, ""), true
	case *types.Pointer:
		elem, ok := p.schemaFromExternalType(tt.Elem())
		if !ok || elem == nil {
			return nil, false
		}
		elem.setIsPointer(true)
		return elem, true
	case *types.Slice:
		item, ok := p.schemaFromExternalType(tt.Elem())
		if !ok || item == nil {
			return nil, false
		}
		return CreateArrayField(item, ""), true
	case *types.Array:
		item, ok := p.schemaFromExternalType(tt.Elem())
		if !ok || item == nil {
			return nil, false
		}
		return CreateArrayField(item, ""), true
	case *types.Map:
		if !isStringType(tt.Key()) {
			return nil, false
		}
		val, ok := p.schemaFromExternalType(tt.Elem())
		if !ok || val == nil {
			return nil, false
		}
		return CreateMapField(val, ""), true
	case *types.Struct:
		return nil, false
	default:
		return nil, false
	}
}

func schemaTypeFromBasic(b *types.Basic) (SchemaType, bool) {
	switch b.Kind() {
	case types.String:
		return SchemaTypeString, true
	case types.Bool:
		return SchemaTypeBoolean, true
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
		types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64, types.Uintptr:
		if b.Name() == "byte" || b.Name() == "rune" {
			return SchemaTypeString, true
		}
		return SchemaTypeInteger, true
	case types.Float32, types.Float64:
		return SchemaTypeNumber, true
	default:
		return SchemaTypeUnknown, false
	}
}

func isStringType(t types.Type) bool {
	t = types.Unalias(t)
	switch tt := t.(type) {
	case *types.Basic:
		return tt.Kind() == types.String
	case *types.Named:
		return isStringType(tt.Underlying())
	default:
		return false
	}
}

func setCustomType(elem SchemaElement, custom string) {
	if elem == nil || custom == "" {
		return
	}
	switch e := elem.(type) {
	case *FieldSchemaElement:
		e.CustomElementType = custom
	case *ArraySchemaElement:
		e.CustomElementType = custom
	case *ObjectSchemaElement:
		e.CustomElementType = custom
	}
}
