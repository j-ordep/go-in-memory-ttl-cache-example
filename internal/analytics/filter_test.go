package analytics

import (
	"encoding/json"
	"strings"
	"testing"
)

func testCatalog() *Snapshot {
	return NewSnapshot([]AttributeDef{
		{Key: "department", DataType: TypeCategory, Storage: "column", Filterable: true},
		{Key: "tenure_months", DataType: TypeNumber, Storage: "derived", Filterable: true},
		{Key: "risk_level", DataType: TypeCategory, Storage: "column", Filterable: true},
		{Key: "salario_atual", DataType: TypeNumber, Storage: "attributes", Filterable: true},
		{Key: "idiomas", DataType: TypeCategory, Storage: "attributes", Filterable: true},
		{Key: "ultima_promocao", DataType: TypeDate, Storage: "attributes", Filterable: true},
		{Key: "insalubre", DataType: TypeBoolean, Storage: "attributes", Filterable: true},
		{Key: "secreto", DataType: TypeText, Storage: "attributes", Filterable: false},
		{Key: "hack", DataType: TypeText, Storage: "column", Filterable: true}, // fora da whitelist
	}, nil)
}

func compile(t *testing.T, js string) (string, []any, error) {
	t.Helper()
	var n FilterNode
	if err := json.Unmarshal([]byte(js), &n); err != nil {
		t.Fatal(err)
	}
	a := &args{}
	sql, err := compileFilter(testCatalog(), &n, a)
	return sql, a.vals, err
}

func TestCompileNestedFilter(t *testing.T) {
	sql, vals, err := compile(t, `{"op":"and","conditions":[
		{"field":"department","operator":"in","value":["Logística","Comercial"]},
		{"field":"tenure_months","operator":"gt","value":60},
		{"op":"or","conditions":[
			{"field":"salario_atual","operator":"lt","value":8000},
			{"field":"idiomas","operator":"eq","value":"Não possui"}
		]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	want := `(em.department = ANY($1::text[]) AND em.tenure_months > $2::numeric AND ` +
		`((CASE WHEN jsonb_typeof(em.attributes->$3::text) = 'number' THEN (em.attributes->>$3::text)::numeric END) < $4::numeric` +
		` OR (em.attributes->>$5::text) = $6::text))`
	if sql != want {
		t.Fatalf("SQL inesperado:\n got: %s\nwant: %s", sql, want)
	}
	if len(vals) != 6 || vals[2] != "salario_atual" || vals[5] != "Não possui" {
		t.Fatalf("parâmetros inesperados: %#v", vals)
	}
}

func TestKeysNeverReachSQL(t *testing.T) {
	// a key do jsonb entra como parâmetro, nunca no texto do SQL
	sql, _, err := compile(t, `{"field":"idiomas","operator":"eq","value":"x'; DROP TABLE employees; --"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sql, "DROP") || strings.Contains(sql, "idiomas") {
		t.Fatalf("valor ou key vazou para o SQL: %s", sql)
	}
}

func TestRejectsInvalidFilters(t *testing.T) {
	cases := map[string]string{
		"campo inexistente":        `{"field":"nao_existe","operator":"eq","value":"x"}`,
		"campo não filtrável":      `{"field":"secreto","operator":"eq","value":"x"}`,
		"coluna fora da whitelist": `{"field":"hack","operator":"eq","value":"x"}`,
		"operador errado p/ tipo":  `{"field":"department","operator":"gt","value":1}`,
		"valor errado p/ tipo":     `{"field":"tenure_months","operator":"gt","value":"muito"}`,
		"data inválida":            `{"field":"ultima_promocao","operator":"gt","value":"01/02/2024"}`,
		"between incompleto":       `{"field":"tenure_months","operator":"between","value":[12]}`,
		"lista vazia":              `{"field":"department","operator":"in","value":[]}`,
		"op desconhecido":          `{"op":"xor","conditions":[]}`,
		"grupo e condição juntos":  `{"op":"and","field":"department","conditions":[]}`,
		"profundidade excessiva":   `{"op":"and","conditions":[{"op":"and","conditions":[{"op":"and","conditions":[{"op":"and","conditions":[{"op":"and","conditions":[{"op":"and","conditions":[{"op":"and","conditions":[]}]}]}]}]}]}]}`,
	}
	for name, js := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := compile(t, js)
			if err == nil {
				t.Fatal("esperava erro")
			}
			if _, ok := IsClientError(err); !ok {
				t.Fatalf("erro deveria ser do cliente (400): %v", err)
			}
		})
	}
}

func TestBooleanDateAndNulls(t *testing.T) {
	for _, js := range []string{
		`{"field":"insalubre","operator":"eq","value":true}`,
		`{"field":"ultima_promocao","operator":"between","value":["2024-01-01","2025-12-31"]}`,
		`{"field":"idiomas","operator":"is_null"}`,
		`{"field":"department","operator":"not_in","value":["RH"]}`,
	} {
		if _, _, err := compile(t, js); err != nil {
			t.Errorf("%s: %v", js, err)
		}
	}
	if err := ValidateFilter(testCatalog(), nil); err != nil {
		t.Fatal(err)
	}
}
