//go:build ignore

// GenContract regenerates focusguard-ui/src/api/types.ts from the Go structs
// that define the IPC contract (internal/transport/ipc + the referenced domain types:
// policy, preset, pomodoro, analytics, schedule, tamper). The Go source is the
// single source of truth — the TypeScript mirror can never drift (C1 of the
// refactoring plan).
//
// Usage (from the repo root):
//
//	go run ./scripts/gen-contract            # rewrite focusguard-ui/src/api/types.ts
//	go run ./scripts/gen-contract --check    # fail if the file is stale (CI)
//
// Mapping rules:
//   - JSON tag name + ",omitempty" -> TS property name + optional "?"
//   - *T (pointer) -> optional "?"
//   - time.Time -> string (RFC3339), time.Duration -> number (nanoseconds)
//   - named string types with const values (ex.: pomodoro.Phase) -> literal union
//   - struct references -> the target's TS name (see targets below)
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
)

// outPath is relative to the repo root (the script runs from there).
const outPath = "focusguard-ui/src/api/types.ts"

// target declares one Go struct mirrored into TypeScript, in emission order.
type target struct {
	file   string // path relative to the repo root
	pkg    string // Go package name
	goType string // Go type name
	tsName string // TS interface name
}

var targets = []target{
	{"internal/domain/policy/policy.go", "policy", "Block", "Block"},
	{"internal/domain/preset/preset.go", "preset", "Preset", "Preset"},
	{"internal/domain/pomodoro/pomodoro.go", "pomodoro", "State", "PomodoroState"},
	{"internal/domain/analytics/analytics.go", "analytics", "DayStat", "DayStat"},
	{"internal/domain/analytics/analytics.go", "analytics", "DomainStat", "DomainStat"},
	{"internal/domain/analytics/analytics.go", "analytics", "Stats", "Stats"},
	{"internal/domain/analytics/analytics.go", "analytics", "LabelStat", "LabelStat"},
	{"internal/domain/analytics/analytics.go", "analytics", "Session", "FocusSession"},
	{"internal/domain/schedule/schedule.go", "schedule", "Rule", "ScheduleRule"},
	{"internal/domain/devices/devices.go", "devices", "Device", "Device"},
	{"internal/domain/achievements/achievements.go", "achievements", "Achievement", "Achievement"},
	{"internal/domain/reports/reports.go", "reports", "Config", "ReportConfig"},
	{"internal/domain/telemetry/telemetry.go", "telemetry", "BlockedQuery", "TelemetryEntry"},
	{"internal/domain/telemetry/telemetry.go", "telemetry", "Summary", "TelemetrySummary"},
	{"internal/infrastructure/tamper/tamper.go", "tamper", "Event", "TamperEvent"},
	{"internal/transport/metrics/metrics.go", "metrics", "ActionStat", "ActionStat"},
	{"internal/transport/ipc/ipc.go", "ipc", "Request", "ApiRequest"},
	{"internal/transport/ipc/ipc.go", "ipc", "Response", "ApiResponse"},
	{"internal/transport/ipc/ipc.go", "ipc", "Event", "Event"},
}

// fieldHints adds short annotations that the Go source does not carry but the
// TS mirror documents (kept deliberately tiny — the structure comes from Go).
var fieldHints = map[string]string{
	"ScheduleRule.days": "0 = domingo",
}

// typeOverrides replaces the TS type of a specific field. Used only when the
// Go source models a documented literal union as a plain string (ex.: tamper
// source/action — see the comments on tamper.Event). If the Go field ever
// gains real constants, move the union to the Go side and drop the override.
var typeOverrides = map[string]string{
	// "clock" é o source do Clock Tamper Protection (Fase 2) e "lockdown" a
	// ação do bloqueio preventivo confirmado por NTP.
	"TamperEvent.source": `"hosts" | "state" | "clock"`,
	"TamperEvent.action": `"restore" | "reconcile" | "lockdown"`,
}

func main() {
	check := flag.Bool("check", false, "fail when types.ts differs from the generated output (CI)")
	flag.Parse()

	out, err := generate()
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}

	if *check {
		cur, err := os.ReadFile(outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "erro: %v — rode `make contract` para gerar\n", err)
			os.Exit(1)
		}
		if string(cur) != out {
			fmt.Fprintln(os.Stderr, "✘ focusguard-ui/src/api/types.ts desatualizado — rode `make contract`")
			os.Exit(1)
		}
		fmt.Println("✔ contrato Go→TS em dia")
		return
	}

	if err := os.WriteFile(outPath, []byte(out), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
	fmt.Println("✔ focusguard-ui/src/api/types.ts regenerado")
}

// generate parses the Go contract files and renders the TS mirror.
func generate() (string, error) {
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	pkgOf := map[string]string{}
	for _, t := range targets {
		if _, ok := files[t.file]; ok {
			continue
		}
		f, err := parser.ParseFile(fset, t.file, nil, parser.ParseComments)
		if err != nil {
			return "", err
		}
		files[t.file] = f
		pkgOf[t.file] = f.Name.Name
	}

	// constStringTypes[pkg][typeName] = ordered literal values of a named
	// string type (ex.: pomodoro.Phase -> ["work", "rest"]).
	constStringTypes := map[string]map[string][]string{}
	for path, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || vs.Type == nil || len(vs.Values) == 0 {
					continue
				}
				id, ok := vs.Type.(*ast.Ident)
				if !ok {
					continue
				}
				for _, v := range vs.Values {
					lit, ok := v.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					val, err := strconv.Unquote(lit.Value)
					if err != nil {
						continue
					}
					if constStringTypes[pkgOf[path]] == nil {
						constStringTypes[pkgOf[path]] = map[string][]string{}
					}
					constStringTypes[pkgOf[path]][id.Name] = append(constStringTypes[pkgOf[path]][id.Name], val)
				}
			}
		}
	}

	tsNames := map[string]string{}
	for _, t := range targets {
		tsNames[t.pkg+"."+t.goType] = t.tsName
	}

	g := &gen{constStringTypes: constStringTypes, tsNames: tsNames}

	var b strings.Builder
	b.WriteString("// Code generated by scripts/gen-contract (make contract). DO NOT EDIT.\n")
	b.WriteString("// Fonte de verdade: os structs Go em internal/transport/ipc e nos pacotes de domínio\n")
	b.WriteString("// referenciados (policy, preset, pomodoro, analytics, schedule, tamper,\n")
	b.WriteString("// metrics).\n")
	b.WriteString("// Edite o Go e rode `make contract` — nunca edite este arquivo à mão.\n")
	b.WriteString("// Regra do AGENT.md: mudou o contrato IPC, atualize CLI + tray + web + este\n")
	b.WriteString("// arquivo no mesmo commit (regenerado).\n")
	b.WriteString("\n")

	for _, t := range targets {
		f := files[t.file]
		spec, gd := findTypeSpec(f, t.goType)
		if spec == nil {
			return "", fmt.Errorf("%s: tipo %s não encontrado", t.file, t.goType)
		}
		s, err := g.emitStruct(f, spec, gd, t.pkg, t.tsName)
		if err != nil {
			return "", fmt.Errorf("%s.%s: %w", t.pkg, t.goType, err)
		}
		b.WriteString(s)
		b.WriteString("\n")
	}

	out := b.String()
	if cur, err := os.ReadFile(outPath); err == nil && strings.Contains(string(cur), "\r\n") {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	return out, nil
}

// findTypeSpec returns the TypeSpec for name in the file (nil if absent) and
// the GenDecl that owns it. The owning GenDecl is needed for the type's doc
// comment: go/ast may attach it to the GenDecl instead of the TypeSpec (as it
// does for analytics.Session).
func findTypeSpec(f *ast.File, name string) (*ast.TypeSpec, *ast.GenDecl) {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if ok && ts.Name.Name == name {
				return ts, gd
			}
		}
	}
	return nil, nil
}

// gen holds the symbol tables needed to translate Go types to TS.
type gen struct {
	constStringTypes map[string]map[string][]string
	tsNames          map[string]string
}

// tsType translates a Go type expression to TS, reporting whether the field is
// optional (pointer) and a short annotation hint (RFC3339 / nanoseconds).
func (g *gen) tsType(expr ast.Expr, pkg string) (ts string, optional bool, hint string, err error) {
	switch e := expr.(type) {
	case *ast.Ident:
		switch e.Name {
		case "string":
			return "string", false, "", nil
		case "bool":
			return "boolean", false, "", nil
		case "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64",
			"float32", "float64":
			return "number", false, "", nil
		}
		// Named string type in the same package (ex.: pomodoro.Phase).
		if vals := g.constStringTypes[pkg][e.Name]; len(vals) > 0 {
			return stringUnion(vals), false, "", nil
		}
		// Named struct in the same package (ex.: DayStat inside analytics.Stats).
		if tsName, ok := g.tsNames[pkg+"."+e.Name]; ok {
			return tsName, false, "", nil
		}
		return "", false, "", fmt.Errorf("tipo nomeado %s sem tradução TS", e.Name)
	case *ast.SelectorExpr:
		x, ok := e.X.(*ast.Ident)
		if !ok {
			return "", false, "", fmt.Errorf("selector inesperado: %s", expr)
		}
		if x.Name == "time" {
			switch e.Sel.Name {
			case "Time":
				return "string", false, "RFC3339", nil
			case "Duration":
				return "number", false, "nanosegundos", nil
			}
		}
		if tsName, ok := g.tsNames[x.Name+"."+e.Sel.Name]; ok {
			return tsName, false, "", nil
		}
		return "", false, "", fmt.Errorf("tipo %s.%s sem tradução TS", x.Name, e.Sel.Name)
	case *ast.ArrayType:
		inner, _, _, err := g.tsType(e.Elt, pkg)
		if err != nil {
			return "", false, "", err
		}
		return inner + "[]", false, "", nil
	case *ast.StarExpr:
		inner, _, hint, err := g.tsType(e.X, pkg)
		if err != nil {
			return "", false, "", err
		}
		return inner, true, hint, nil
	}
	return "", false, "", fmt.Errorf("expressão de tipo não suportada: %s", expr)
}

// emitStruct renders one TS interface from a Go struct declaration.
func (g *gen) emitStruct(f *ast.File, spec *ast.TypeSpec, gd *ast.GenDecl, pkg, tsName string) (string, error) {
	st, ok := spec.Type.(*ast.StructType)
	if !ok {
		return "", fmt.Errorf("%s não é struct", spec.Name.Name)
	}

	var b strings.Builder
	// go/ast pode anexar o doc ao GenDecl em vez do TypeSpec (ex.:
	// analytics.Session) — usa o primeiro disponível.
	doc := spec.Doc
	if doc == nil && gd != nil {
		doc = gd.Doc
	}
	if doc != nil {
		if line := firstLine(doc.Text()); line != "" {
			fmt.Fprintf(&b, "// %s\n", line)
		}
	}
	fmt.Fprintf(&b, "export interface %s {\n", tsName)

	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			continue // embedded field — none in the contract structs
		}
		jsonTag := ""
		if field.Tag != nil {
			jsonTag = reflect.StructTag(strings.Trim(field.Tag.Value, "`")).Get("json")
		}
		if jsonTag == "-" {
			continue
		}
		name := field.Names[0].Name
		omitempty := false
		if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			name = parts[0]
			for _, p := range parts[1:] {
				if p == "omitempty" {
					omitempty = true
				}
			}
		}

		ts, optional, hint, err := g.tsType(field.Type, pkg)
		if err != nil {
			return "", err
		}
		if override, ok := typeOverrides[tsName+"."+name]; ok {
			ts = override
			hint = ""
		}
		optional = optional || omitempty

		comment := ""
		if field.Doc != nil {
			comment = firstLine(field.Doc.Text())
		} else if hint != "" {
			comment = hint
		}
		if comment == "" {
			comment = fieldHints[tsName+"."+name]
		}

		q := ""
		if optional {
			q = "?"
		}
		fmt.Fprintf(&b, "  %s%s: %s", name, q, ts)
		if comment != "" {
			fmt.Fprintf(&b, "; // %s", comment)
		} else {
			b.WriteString(";")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return b.String(), nil
}

// stringUnion renders literal values as a TS union ("work" | "rest").
func stringUnion(vals []string) string {
	quoted := make([]string, 0, len(vals))
	for _, v := range vals {
		quoted = append(quoted, `"`+v+`"`)
	}
	return strings.Join(quoted, " | ")
}

// firstLine returns the first non-empty line of a doc comment text.
func firstLine(text string) string {
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
