package analytics

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidField  = errors.New("campo inválido")
	ErrInvalidFilter = errors.New("filtro inválido")
	ErrInvalidParam  = errors.New("parâmetro inválido")
	ErrNotFound      = errors.New("não encontrado")
)

// args acumula os parâmetros posicionais ($1, $2...) de uma consulta.
type args struct{ vals []any }

func (a *args) add(v any) string {
	a.vals = append(a.vals, v)
	return fmt.Sprintf("$%d", len(a.vals))
}

// viewColumns é a whitelist das colunas da view employee_month que podem
// ser referenciadas por storage = column ou derived. Uma key só vira
// identificador SQL se estiver aqui; nada vindo do request é concatenado.
var viewColumns = map[string]string{
	"employee_id":     "em.employee_id",
	"name":            "em.name",
	"gender":          "em.gender",
	"birth_date":      "em.birth_date",
	"admission_date":  "em.admission_date",
	"department":      "em.department",
	"job_title":       "em.job_title",
	"region":          "em.region",
	"age":             "em.age",
	"tenure_months":   "em.tenure_months",
	"risk_score":      "em.risk_score",
	"risk_level":      "em.risk_level",
	"total_risk":      "em.total_risk",
	"total_retention": "em.total_retention",
	"net_balance":     "em.net_balance",
}

// fieldExpr devolve a expressão SQL de um atributo sobre a view
// employee_month (alias em), já com o tipo certo:
//
//	column/derived -> em.department
//	attributes     -> (em.attributes->>$n::text)::numeric  (cast pelo data_type)
//
// Para chaves do jsonb, a key entra como parâmetro. Os casts são protegidos
// por CASE: um valor mal normalizado pela IA vira NULL em vez de derrubar
// a consulta inteira.
func fieldExpr(def AttributeDef, a *args) (string, error) {
	switch def.Storage {
	case "column", "derived":
		col, ok := viewColumns[def.Key]
		if !ok {
			return "", fmt.Errorf("%w: %q não existe na view employee_month", ErrInvalidField, def.Key)
		}
		return col, nil

	case "attributes":
		k := a.add(def.Key) + "::text"
		raw := fmt.Sprintf("(em.attributes->>%s)", k)
		switch def.DataType {
		case TypeNumber:
			return fmt.Sprintf("(CASE WHEN jsonb_typeof(em.attributes->%s) = 'number' THEN %s::numeric END)", k, raw), nil
		case TypeBoolean:
			return fmt.Sprintf("(CASE WHEN jsonb_typeof(em.attributes->%s) = 'boolean' THEN %s::boolean END)", k, raw), nil
		case TypeDate:
			return fmt.Sprintf(`(CASE WHEN %s ~ '^\d{4}-\d{2}-\d{2}$' THEN %s::date END)`, raw, raw), nil
		default:
			return raw, nil
		}
	}
	return "", fmt.Errorf("%w: storage %q desconhecido", ErrInvalidField, def.Storage)
}

func sqlType(t DataType) string {
	switch t {
	case TypeNumber:
		return "numeric"
	case TypeDate:
		return "date"
	case TypeBoolean:
		return "boolean"
	default:
		return "text"
	}
}
