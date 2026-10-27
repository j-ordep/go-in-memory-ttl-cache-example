# 03 — Campos dinâmicos e parâmetros SQL

Código: [fields.go](../internal/analytics/fields.go). Quem usa essas funções está em [filter.go](../internal/analytics/filter.go) e [repository.go](../internal/analytics/repository.go).

## 1. Dinâmico não significa sem definição

Um registro pode conter:

```json
{
  "salario_atual": 4500,
  "idiomas": "Inglês",
  "insalubre": false
}
```

Esse objeto representa um possível valor da coluna `attributes`. Sozinho, ele não explica quais campos a aplicação permite consultar, qual unidade usar ou quais operadores aceitar.

Uma definição ilustrativa no catálogo seria:

```json
{
  "key": "salario_atual",
  "dataType": "number",
  "storage": "attributes",
  "unit": "currency",
  "filterable": true,
  "simpleFilter": false,
  "displayOrder": 10
}
```

A chave conecta a definição ao valor. O backend não precisa adivinhar que “salário” é um número: usa a definição carregada.

O código deste exemplo lê os dados e os metadados. Ele não contém o importador que preenche essas tabelas.

## 2. Três origens, uma função de tradução

`fieldExpr(def, a)` recebe uma definição e devolve a expressão SQL para ler o campo.

| Storage | Caminho neste código |
|---|---|
| `column` | Procura a chave na lista explícita `viewColumns`. |
| `derived` | Faz a mesma procura na lista; o cálculo já precisa estar disponível na view. |
| `attributes` | Extrai uma chave do JSONB e considera `DataType`. |

O retorno é uma expressão, não uma consulta completa e nem o valor final.

Exemplo: `em.department` significa “a coluna department da origem que recebeu o apelido em”. A parte `FROM employee_month em` aparece depois, na consulta do repository.

## 3. Por que existe viewColumns

```go
var viewColumns = map[string]string{
    "department":    "em.department",
    "tenure_months": "em.tenure_months",
    // Outros campos estão no arquivo original.
}
```

Essas são expressões escritas pelo desenvolvedor. Um nome enviado pelo cliente só é traduzido para coluna quando existe nesse mapa.

Se alguém enviar uma chave desconhecida ou cadastrar um campo de coluna não previsto, `fieldExpr` retorna erro. O código não usa o texto do request diretamente como nome de coluna.

Um parâmetro SQL representa um valor; ele não substitui livremente a sintaxe de um identificador de coluna. Por isso existe essa separação: colunas vêm de expressões permitidas; valores seguem como parâmetros.

## 4. args: a lista que acompanha a consulta

```go
type args struct{ vals []any }

func (a *args) add(v any) string {
    a.vals = append(a.vals, v)
    return fmt.Sprintf("$%d", len(a.vals))
}
```

`any` permite que a lista reúna valores de tipos diferentes, como datas, strings e números. A responsabilidade de interpretar cada posição é do SQL e do driver.

Se executarmos conceitualmente:

```go
a := &args{}
first := a.add("Comercial") // "$1"
second := a.add(12)        // "$2"
```

O estado fica:

```text
first  = "$1"
second = "$2"
a.vals = ["Comercial", 12]
```

`add` não coloca aspas nem transforma o valor em texto SQL. Ele guarda o valor e devolve o marcador da posição.

Depois o repository passa as duas partes:

```go
rows, err := r.db.Query(ctx, sql, a.vals...)
```

O `...` expande o slice como argumentos da chamada. O driver recebe os valores separadamente da string da consulta.

## 5. Uma chave de JSON também é um valor parametrizado

Para atributos, o código faz:

```go
k := a.add(def.Key) + "::text"
raw := fmt.Sprintf("(em.attributes->>%s)", k)
```

Se a próxima posição for 3 e a chave for `salario_atual`:

```text
k = "$3::text"
raw = "(em.attributes->>$3::text)"
a.vals[2] = "salario_atual"
```

A posição no SQL começa em 1; o índice do slice Go começa em 0. Portanto `$3` usa o terceiro item, `a.vals[2]`.

No PostgreSQL, `->` extrai um valor JSON e `->>` extrai seu conteúdo como texto. `jsonb_typeof` permite verificar o tipo JSON. São operadores e funções descritos na [documentação de JSON do PostgreSQL](https://www.postgresql.org/docs/current/functions-json.html).

## 6. Por que converter o tipo antes de comparar

Textos e números não têm a mesma ordenação. Para comparar valores numéricos, a expressão precisa ser numérica.

Para `TypeNumber`, a expressão produzida é equivalente a:

```sql
CASE
  WHEN jsonb_typeof(em.attributes->$3::text) = 'number'
  THEN (em.attributes->>$3::text)::numeric
END
```

Leia assim:

1. Localize no JSON a chave passada em `$3`.
2. Verifique se o valor JSON é um número.
3. Extraia o conteúdo textual e converta para `numeric`.
4. Se a condição não for satisfeita, o `CASE` sem `ELSE` resulta em SQL `NULL`.

Isso permite comparar 2 e 10 como números. Não é uma conversão automática de qualquer string para número.

| Valor no JSON para uma definição numérica | Resultado dessa expressão |
|---|---|
| `4500` | Número 4500. |
| `0` | Número zero válido. |
| `"4500"` | SQL `NULL`: o JSON contém texto. |
| `null` ou chave ausente | SQL `NULL`. |
| `""` | SQL `NULL`: não vira zero. |

Para uma definição de texto, uma string vazia pode continuar sendo texto vazio. Ausência, zero e texto vazio não são o mesmo estado.

## 7. Os outros tipos

- **Boolean:** exige `jsonb_typeof(...) = 'boolean'` antes do cast.
- **Date:** verifica o formato textual `AAAA-MM-DD` com expressão regular e aplica `::date`.
- **Text e category:** usam a extração textual.

A proteção de data tem um limite importante: uma expressão regular de formato não comprova que a data existe. Um texto como `2026-02-31` passa pelo formato e pode falhar no cast. A documentação descreve o código existente; não há uma validação completa de calendário dentro de `fieldExpr`.

`sqlType` é uma função diferente: ela fornece `numeric`, `date`, `boolean` ou `text` para os parâmetros das comparações.

## 8. Onde mês e identidade entram na numeração

O repository cria uma única instância de `args` para a consulta. `scope` adiciona primeiro o mês e a identidade.

Se o filtro for “salario_atual maior que 4500”, a sequência é:

| Posição | Conteúdo ilustrativo | Quem adiciona |
|---|---|---|
| `$1` | Primeiro dia do mês como `time.Time`. | `scope` |
| `$2` | `"ana"` | `scope` |
| `$3` | `"salario_atual"` | `fieldExpr` |
| `$4` | `4500` como valor numérico Go. | Compilador do filtro. |

Depois podem entrar novos argumentos para ordenação, limite e offset. A mesma lista evita reutilizar acidentalmente uma posição com outro significado.

## 9. Como isso se conecta ao catálogo

```text
Campo recebido
  → Snapshot.Attribute(chave)
  → AttributeDef conhecido
  → fieldExpr(def, args)
  → expressão com tipo adequado
  → comparação ou ordenação na consulta
```

A chave precisa vir do catálogo para que o backend conheça sua origem e tipo. Um texto amigável na interface é outra preocupação; ele não substitui essa chave técnica.

Não há uma regra automática para afirmar que chaves diferentes em empresas diferentes representam o mesmo conceito. Também não há normalização de escalas em `fieldExpr`.

## Confira se entendeu

1. `fieldExpr` executa SQL? Não; monta uma expressão.
2. `args.add` concatena o valor do usuário na consulta? Não; guarda o valor e devolve a posição.
3. Todo número escrito entre aspas no JSON será convertido? Não; o tipo JSON é considerado.
4. Um mapa de metadados elimina a necessidade de validar colunas fixas? Não; `viewColumns` continua definindo as expressões aceitas.

[Anterior: catálogo](02-catalogo-snapshot-e-cache.md) · [Índice](README.md) · [Próximo: filtros](04-filtros-passo-a-passo.md)
