# 05 — Repository e consultas

Código: [repository.go](../internal/analytics/repository.go).

## 1. O papel do repository

O repository reúne a lógica de acesso aos dados. Ele recebe parâmetros já representados em tipos Go, pede definições ao catálogo, monta consultas e converte os resultados.

```go
type Repo struct {
    db  *pgxpool.Pool
    cat *Catalog
}
```

`db` permite consultar dados. `cat` permite interpretar campos. Ter o catálogo em cache não significa que as linhas de funcionários estejam em cache.

Os dois campos são ponteiros para objetos criados em `main`. O construtor `NewRepo` apenas guarda essas dependências.

## 2. ResolveMonth escolhe a competência

“Competência” é o mês ao qual os dados se referem.

Quando recebe texto:

1. Tenta interpretar `AAAA-MM`.
2. Tenta interpretar `AAAA-MM-DD`.
3. Normaliza para o primeiro dia daquele mês, em UTC.
4. Se não conseguir interpretar, devolve `ErrInvalidParam`.

Quando recebe string vazia, consulta a maior `reference_month` em `competencies` cujo status seja `done`. Sem mês disponível, devolve `ErrNotFound`.

A escolha do mês pode, portanto, executar uma consulta mesmo quando o catálogo já está em cache.

## 3. scope monta o recorte compartilhado

A função `scope` prepara uma base usada por listagem e várias consultas analíticas:

1. Exige que `Viewer.Login` não esteja vazio.
2. Obtém um snapshot com `r.cat.Get(ctx)`.
3. Cria uma lista `args`.
4. Adiciona o mês e o login.
5. Monta as condições obrigatórias.
6. Compila o filtro opcional com o mesmo snapshot e a mesma lista.
7. Devolve snapshot, argumentos e expressão `WHERE`.

A base tem esta forma:

```sql
em.reference_month = $1::date
AND em.visible_to @> ARRAY[$2::text]
AND /* expressão do filtro, ou TRUE */
```

`@>` verifica se o array de visibilidade contém o login. Mês e identidade não vêm como condições comuns escolhidas pelo cliente.

Se o filtro contém um `OR`, os parênteses produzidos pelo compilador mantêm esse grupo dentro da parte de filtro. O grupo não substitui as condições externas de mês e visibilidade.

O texto `"admin"` usado em testes é apenas um login. Não existe um desvio especial de administrador dentro de `scope`.

## 4. Os tipos de entrada e saída da listagem

`SearchParams` reúne `Month`, `Filter`, `SortBy`, `SortDesc`, `Limit` e `Offset`.

`EmployeeRow` representa uma linha devolvida. Campos opcionais usam ponteiros; `Attributes` usa `json.RawMessage` para preservar o objeto JSON sem exigir uma struct fixa para cada chave dinâmica.

```go
type Page[T any] struct {
    Total int
    Items []T
}
```

Esse é um tipo genérico. Em `Page[EmployeeRow]`, o parâmetro `T` é substituído por `EmployeeRow`. As tags do arquivo original fazem a resposta usar `total` e `items`.

O retorno de uma busca é uma página de registros, não um snapshot do catálogo.

## 5. SearchEmployees, em ordem

### Preparar o recorte

Chama `scope` e recebe uma versão do catálogo, a expressão de filtro e os argumentos já acumulados.

### Preparar a ordenação

Sem `SortBy`, usa `em.risk_score DESC`.

Com `SortBy`:

1. Procura a chave em `cat.Attribute`.
2. Rejeita chave desconhecida.
3. Traduz a definição por `fieldExpr`.
4. Usa `ASC`, ou `DESC` quando `SortDesc` for verdadeiro.

A direção vem dessas duas strings conhecidas. Valores nulos ficam por último e `employee_id` desempata a ordenação.

Uma definição numérica dinâmica vira expressão numérica; ordenar por ela não é ordenar o texto bruto do JSON.

A busca por chave na ordenação não verifica `Filterable`. Esse flag é aplicado nas condições de filtro. Não trate “filtrável”, “ordenável” e “visível” como uma permissão única: o código usa cada informação em lugares diferentes.

### Ajustar a paginação

- `Limit <= 0` ou `Limit > 500` vira 50.
- `Offset < 0` vira 0.
- Os dois valores são adicionados como parâmetros.

`Offset` é a quantidade de registros a pular; não é o número da página. Uma interface de páginas pode calcular `offset = (página - 1) × limite`.

### Executar a consulta

Trecho reduzido para mostrar a estrutura:

```sql
SELECT em.employee_id, em.name, count(*) OVER()
FROM employee_month em
WHERE /* mês, visibilidade e filtro */
ORDER BY /* expressão conhecida */ DESC NULLS LAST, em.employee_id
LIMIT /* parâmetro */ OFFSET /* parâmetro */
```

A lista real de colunas está em `employeeColumns`.

## 6. O que count(*) OVER() faz

Essa expressão acrescenta a contagem total do conjunto filtrado a cada linha da página. A contagem considera o conjunto antes de limitar os registros devolvidos.

Se há 120 registros correspondentes e o limite é 20, cada linha recebida informa o mesmo total: 120. Assim o código obtém registros e total em uma consulta.

Existe uma consequência no exemplo: se a página vier vazia, nenhuma linha traz o total. `page.Total` permanece zero.

Por isso uma busca com `offset` além do último registro pode devolver `total: 0` mesmo havendo resultados em páginas anteriores. É uma limitação existente, não uma garantia de paginação a copiar sem avaliar.

## 7. Como uma linha do banco vira uma struct

`scanEmployee` prepara os endereços dos campos e chama `Scan`.

```go
dest := []any{&e.ID, &e.Name /* demais destinos no arquivo */ }
err := row.Scan(dest...)
```

A ordem das colunas deve corresponder à ordem dos destinos. `&e.Name` é o endereço onde o driver escreverá o nome lido.

Na listagem, o argumento extra recebe a contagem da janela. No detalhe, argumentos extras recebem outros campos. O `extra ...any` permite reaproveitar a leitura das colunas comuns.

`SearchEmployees`:

1. Executa `Query`.
2. Agenda `rows.Close()`.
3. Percorre `rows.Next()`.
4. Converte cada linha e acrescenta o registro a `Items`.
5. Consulta `rows.Err()` para detectar falhas durante a iteração.

A lista de itens começa como slice vazio, para representar uma página sem itens como `[]`.

## 8. AttributeOptions: valores que existem nos dados

O catálogo diz que um campo existe. `AttributeOptions` consulta quais valores desse campo aparecem para o usuário naquele mês.

Ela aplica `scope`, procura a definição, obtém a expressão e faz `SELECT DISTINCT`, excluindo SQL `NULL`, com ordenação e limite de 1000 opções.

O código rejeita explicitamente tipos number e date. Apesar da mensagem mencionar categorias, outros tipos não rejeitados, como texto e booleano, também seguem para a consulta. Ele não verifica `Filterable` nesse método.

Assim, opções não são o mesmo que definições nem são uma lista fixa guardada dentro do snapshot.

## 9. Overview e Breakdown: reaproveitar o recorte

### Overview

Usa `scope` e executa agregações: total, contagens por nível de risco e médias. O filtro é aplicado antes das agregações.

### Breakdown

Usa o mesmo recorte e escolhe um campo para agrupar:

- número: usa `width_bucket` e as faixas informadas;
- data: agrupa pelo ano extraído;
- outros tipos: agrupa pelo valor textual.

Para número, os limites precisam estar ordenados, com quantidade entre 1 e 20. Por exemplo, `[12, 36]` forma os grupos abaixo de 12, de 12 até antes de 36, e a partir de 36.

Depois o código traduz os índices das faixas para `bucketFrom` e `bucketTo`. Um limite nulo representa ausência de limite daquele lado; não significa valor zero.

## 10. Fatores: metadados versus observações

`risk_factors`, no catálogo, descreve os fatores. `prediction_factors` contém observações por funcionário e mês, como contribuição, direção e valor da variável.

### FactorsSummary

1. Aplica `scope`.
2. Consulta os totais do recorte.
3. Agrupa observações de fatores.
4. Cria outro mapa, `byKey`, com os resultados.
5. Percorre `cat.Factors` para preservar sua ordem.
6. Inclui fatores visíveis que tenham resultado.
7. Acrescenta tipo e unidade vindos do catálogo.

Esse mapa não é um novo cache. É um índice temporário daquela chamada, usado para cruzar resultados com definições.

### FactorScatter

Procura um fator com `cat.Factor(key)`, consulta os pontos e devolve definição e observações. O valor da variável alimenta um eixo; a contribuição alimenta o outro.

O método verifica a existência da chave, mas não aplica `Visible` como `FactorsSummary`. Esse é outro motivo para ler as regras no ponto onde são usadas, sem supor que a presença no catálogo já as executou.

## 11. EmployeeDetail: compor uma resposta com várias consultas

O detalhe começa consultando o funcionário no mês com `scope`. Ausência vira `ErrNotFound`.

Depois carrega:

1. Fatores daquele funcionário e mês; aplica existência no catálogo e `Visible`.
2. Histórico dos meses em que o login está em `visible_to`.
3. Eventos do funcionário, limitados a 200.
4. Sugestões vinculadas ao funcionário e suas versões de prompt.

Nem todas essas consultas usam o mesmo recorte: histórico percorre meses visíveis; eventos e sugestões são consultados pelo funcionário. Não há filtro mensal nesses dois últimos trechos.

As consultas são sequenciais, sem transação explícita envolvendo toda a resposta. A composição reutiliza um snapshot do catálogo, mas isso não torna todos os dados consultados uma fotografia transacional única.

## 12. Indicadores de movimentação: uma parte específica do domínio

As funções `MonthlyTurnover` e `AnnualTurnover` são exemplos de cálculos de domínio existentes no mesmo arquivo.

A primeira lê `monthly_turnover` e calcula a taxa mensal usando saídas e média entre os quantitativos anterior e atual. Sem base anterior, a taxa pode ficar nula.

A segunda reúne os meses do ano, calcula a média dos quantitativos, soma as saídas e calcula taxas acumulada e anualizada. A anualização usa o número do último mês disponível, não apenas a quantidade de linhas.

Essas funções não chamam `scope` e não aplicam `visible_to`; são uma exceção explícita no exemplo. A definição da origem SQL e os dados necessários não estão incluídos no repositório.

Para estudar o mecanismo genérico de catálogo e filtros, você pode ler essa parte depois das demais.

## Confira se entendeu

1. O cache guarda a página da busca? Não.
2. A mesma lista de parâmetros acompanha mês, identidade e filtro? Sim.
3. O total vem de uma segunda consulta na busca? Não; vem da função de janela.
4. Uma página vazia sempre permite descobrir o total por esse método? Não.
5. Todas as consultas do arquivo aplicam `scope`? Não; observe as exceções descritas.

[Anterior: filtros](04-filtros-passo-a-passo.md) · [Índice](README.md) · [Próximo: HTTP e fluxo completo](06-http-e-fluxo-completo.md)
