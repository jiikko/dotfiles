package main

import "github.com/xuri/excelize/v2"

// DefinedName is one workbook/sheet-scoped name (named range, LAMBDA arg, ...).
type DefinedName struct {
	Name     string
	Scope    string // "workbook" or a sheet name
	RefersTo string
}

func extractDefinedNames(xl *excelize.File) []DefinedName {
	var out []DefinedName
	for _, dn := range xl.GetDefinedName() {
		scope := dn.Scope
		if scope == "" || scope == "Workbook" {
			scope = "workbook"
		}
		out = append(out, DefinedName{Name: dn.Name, Scope: scope, RefersTo: dn.RefersTo})
	}
	return out
}
