package analytics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// FilterNode é a árvore do filtro avançado (e do simples, que é só um "and").
//
//	{"op": "and", "conditions": [
//	   {"field": "department", "operator": "in", "value": ["Logística", "Comercial"]},
//	   {"field": "tenure_months", "operator": "gt", "value": 60},
//	   {"op": "or", "conditions": [...]}
//	]}
//
// O mesmo JSON é guardado em saved_filters.definition.
type FilterNode struct {
	Op         string          `json:"op,omitempty"`
	Conditions []FilterNode    `json:"conditions,omitempty"`
	Field      string          `json:"field,omitempty"`
	Operator   string          `json:"operator,omitempty"`
	Value      json.RawMessage `json:"value,omitempty"`
}

const (
	maxFilterDepth      = 5
	maxFilterConditions = 50
)

func set(ops ...string) map[string]bool {
	m := make(map[string]bool, len(ops))
	for _, o := range ops {
		m[o] = true
	}
	return m
}

// Operadores válidos por tipo. O frontend usa a mesma tabela para montar
// os selects, a partir do data_type do catálogo.
var operatorsByType = map[DataType]map[string]bool{
	TypeCategory: set("eq", "neq", "in", "not_in", "is_null", "not_null"),
	TypeText:     set("eq", "neq", "contains", "in", "not_in", "is_null", "not_null"),
	TypeNumber:   set("eq", "neq", "gt", "gte", "lt", "lte", "between", "is_null", "not_null"),
	TypeDate:     set("eq", "neq", "gt", "gte", "lt", "lte", "between", "is_null", "not_null"),
	TypeBoolean:  set("eq", "is_null", "not_null"),
}

var comparison = map[string]string{
	"eq": "=", "neq": "IS DISTINCT FROM", "gt": ">", "gte": ">=", "lt": "<", "lte": "<=",
}

// ValidateFilter confere a árvore contra o catálogo sem executar nada.
// Use antes de gravar em saved_filters.
func ValidateFilter(cat *Snapshot, n *FilterNode) error {
	_, err := compileFilter(cat, n, &args{})
	return err
}

func compileFilter(cat *Snapshot, n *FilterNode, a *args) (string, error) {
	if n == nil {
		return "TRUE", nil
	}
	c := &filterCompiler{cat: cat, a: a}
	return c.node(n, 0)
}

type filterCompiler struct {
	cat   *Snapshot
	a     *args
	count int
}

func (c *filterCompiler) node(n *FilterNode, depth int) (string, error) {
	if depth > maxFilterDepth {
		return "", fmt.Errorf("%w: mais de %d níveis de grupos", ErrInvalidFilter, maxFilterDepth)
	}

	if n.Op != "" {
		if n.Field != "" {
			return "", fmt.Errorf("%w: um nó não pode ser grupo e condição ao mesmo tempo", ErrInvalidFilter)
		}
		var joiner string
		switch n.Op {
		case "and":
			joiner = " AND "
		case "or":
			joiner = " OR "
		default:
			return "", fmt.Errorf("%w: op %q (use and/or)", ErrInvalidFilter, n.Op)
		}
		if len(n.Conditions) == 0 {
			return "TRUE", nil
		}
		parts := make([]string, 0, len(n.Conditions))
		for i := range n.Conditions {
			p, err := c.node(&n.Conditions[i], depth+1)
			if err != nil {
				return "", err
			}
			parts = append(parts, p)
		}
		return "(" + strings.Join(parts, joiner) + ")", nil
	}

	return c.condition(n)
}

func (c *filterCompiler) condition(n *FilterNode) (string, error) {
	c.count++
	if c.count > maxFilterConditions {
		return "", fmt.Errorf("%w: mais de %d condições", ErrInvalidFilter, maxFilterConditions)
	}

	def, ok := c.cat.Attribute(n.Field)
	if !ok || !def.Filterable {
		return "", fmt.Errorf("%w: campo %q não é filtrável", ErrInvalidFilter, n.Field)
	}
	if !operatorsByType[def.DataType][n.Operator] {
		return "", fmt.Errorf("%w: operador %q não vale para %q (%s)", ErrInvalidFilter, n.Operator, n.Field, def.DataType)
	}

	expr, err := fieldExpr(def, c.a)
	if err != nil {
		return "", err
	}
	typ := sqlType(def.DataType)

	switch n.Operator {
	case "is_null":
		return expr + " IS NULL", nil
	case "not_null":
		return expr + " IS NOT NULL", nil

	case "in", "not_in":
		vals, err := decodeStringList(n.Value)
		if err != nil || len(vals) == 0 {
			return "", fmt.Errorf("%w: %q espera uma lista não vazia de valores", ErrInvalidFilter, n.Field)
		}
		p := c.a.add(vals)
		if n.Operator == "in" {
			return fmt.Sprintf("%s = ANY(%s::text[])", expr, p), nil
		}
		// not_in mantém quem não tem o campo preenchido
		return fmt.Sprintf("NOT COALESCE(%s = ANY(%s::text[]), false)", expr, p), nil

	case "between":
		var pair []json.RawMessage
		if err := json.Unmarshal(n.Value, &pair); err != nil || len(pair) != 2 {
			return "", fmt.Errorf("%w: between em %q espera [de, até]", ErrInvalidFilter, n.Field)
		}
		v1, err := decodeScalar(def, pair[0])
		if err != nil {
			return "", err
		}
		v2, err := decodeScalar(def, pair[1])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s BETWEEN %s::%s AND %s::%s", expr, c.a.add(v1), typ, c.a.add(v2), typ), nil

	case "contains":
		var s string
		if err := json.Unmarshal(n.Value, &s); err != nil || s == "" {
			return "", fmt.Errorf("%w: contains em %q espera um texto", ErrInvalidFilter, n.Field)
		}
		return fmt.Sprintf(`%s ILIKE '%%' || %s::text || '%%'`, expr, c.a.add(escapeLike(s))), nil

	default: // eq, neq, gt, gte, lt, lte
		v, err := decodeScalar(def, n.Value)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s %s %s::%s", expr, comparison[n.Operator], c.a.add(v), typ), nil
	}
}

// decodeScalar converte o valor JSON para o tipo Go que o pgx envia
// corretamente para o cast do SQL.
func decodeScalar(def AttributeDef, raw json.RawMessage) (any, error) {
	bad := func() (any, error) {
		return nil, fmt.Errorf("%w: valor inválido para %q (%s)", ErrInvalidFilter, def.Key, def.DataType)
	}
	switch def.DataType {
	case TypeNumber:
		var f float64
		if json.Unmarshal(raw, &f) != nil {
			return bad()
		}
		return f, nil
	case TypeBoolean:
		var b bool
		if json.Unmarshal(raw, &b) != nil {
			return bad()
		}
		return b, nil
	case TypeDate:
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return bad()
		}
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return bad()
		}
		return t, nil
	default:
		s, ok := looseString(raw)
		if !ok {
			return bad()
		}
		return s, nil
	}
}

// decodeStringList aceita ["a","b"] ou [1367, 370] (códigos numéricos de categoria).
func decodeStringList(raw json.RawMessage) ([]string, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		s, ok := looseString(it)
		if !ok {
			return nil, fmt.Errorf("valor de lista inválido")
		}
		out = append(out, s)
	}
	return out, nil
}

func looseString(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	var f json.Number
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&f) == nil {
		return f.String(), true
	}
	return "", false
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
