# Go Dynamic Data Lab

Projeto de estudo em Go sobre catálogos dinâmicos, filtros SQL parametrizados e cache em memória com TTL.

Este repositório reúne um exemplo de API para consultar dados com campos dinâmicos. O domínio de funcionários é usado como demonstração dos padrões, que podem servir de referência para outros projetos.

## Trilha de aprendizado

Para aprender o funcionamento passo a passo, comece pelo [mapa de leitura e localização dos arquivos](docs/00-mapa-de-leitura.md). O [índice da documentação](docs/README.md) organiza os capítulos e o [glossário](docs/08-glossario.md) explica os termos usados.

1. [Fundamentos de Go e inicialização](docs/01-fundamentos-e-inicializacao.md).
2. [Catálogo, snapshot e cache](docs/02-catalogo-snapshot-e-cache.md): construtor, listas, mapas por chave, ponteiros, TTL, mutex, recarga e invalidação.
3. [Campos dinâmicos e parâmetros SQL](docs/03-campos-dinamicos-e-sql.md).
4. [Filtros passo a passo](docs/04-filtros-passo-a-passo.md).
5. [Repository e consultas](docs/05-repository-e-consultas.md).
6. [HTTP e um fluxo completo de requisição](docs/06-http-e-fluxo-completo.md).
7. [Testes e exercícios de leitura](docs/07-testes-e-exercicios.md).

Cada capítulo aponta para o código correspondente, explica o motivo das peças e acompanha exemplos. Você pode seguir a trilha sem conectar a um banco.

## O que estudar

- Catálogo de metadados para descrever o tipo, a origem e a unidade de cada campo.
- Combinação de colunas fixas, campos derivados e atributos armazenados em JSONB.
- Compilação de filtros JSON com grupos `AND` e `OR` em SQL parametrizado.
- Validação de campos e operadores a partir das definições do catálogo.
- Cache local com TTL, carregamento sob demanda e invalidação explícita.
- Organização de uma API HTTP com `net/http`, PostgreSQL e `pgx`.

## Estrutura

```text
cmd/api/main.go                     inicialização e configuração
internal/analytics/catalog.go       catálogo, snapshots e cache
internal/analytics/fields.go        campos do catálogo e expressões SQL tipadas
internal/analytics/filter.go        validação e compilação de filtros
internal/analytics/repository.go    consultas e agregações do exemplo
internal/httpapi/server.go          rotas, handlers e respostas HTTP
```

Uma sequência útil de leitura é `catalog.go` → `fields.go` → `filter.go` → `repository.go` → `server.go`.

## Como o catálogo e o cache funcionam

As tabelas `attribute_definitions` e `risk_factors` fornecem os metadados. A API carrega essas definições em um `Snapshot` com listas e mapas de consulta por chave.

O catálogo é compartilhado pelas requisições dentro do mesmo processo. O TTL configurado em `main.go` é de **um minuto**, contado desde a última carga bem-sucedida.

```text
Requisição pede o catálogo
  ├─ Snapshot presente e dentro do TTL → reutiliza o objeto em memória
  └─ Snapshot ausente ou vencido       → consulta o banco e cria outro snapshot
```

- A atualização acontece sob demanda; não há uma rotina recarregando o catálogo a cada minuto.
- `Invalidate()` marca o catálogo como vencido. A próxima chamada tenta recarregá-lo.
- Se a recarga falhar e existir um snapshot anterior, o exemplo continua usando essa versão. Sem uma versão anterior, retorna erro.
- Um mutex serializa o acesso ao cache durante a verificação e a recarga.
- O snapshot é tratado como somente leitura. Requisições que já receberam a versão anterior podem terminar usando essa versão.
- Depois que o snapshot antigo deixa de ser referenciado, o coletor de lixo do Go pode liberar sua memória.
- Cada processo tem seu próprio cache; reiniciar a API perde o conteúdo em memória.

A rota `POST /internal/catalog/reload` apenas invalida o cache. A consulta ao banco ocorre na próxima chamada ao catálogo.

## Como os filtros funcionam

O cliente envia os campos e operadores desejados. O backend usa o catálogo para validar o filtro e construir a consulta com parâmetros.

Exemplo de corpo para `POST /api/employees/search`:

```json
{
  "month": "2026-03",
  "filter": {
    "op": "and",
    "conditions": [
      { "field": "department", "operator": "eq", "value": "Comercial" },
      { "field": "tenure_months", "operator": "gt", "value": 12 }
    ]
  }
}
```

As chaves precisam existir no catálogo e aceitar filtragem. O armazenamento e o tipo de cada definição determinam como o campo será consultado.

## Executar localmente

Requisitos: Go **1.23 ou superior** e PostgreSQL com schema e dados compatíveis com as consultas deste exemplo.

Este repositório não inclui migrations nem dados de exemplo. As consultas em `repository.go` e os testes de integração descrevem as estruturas e os dados esperados; um banco PostgreSQL vazio não é suficiente.

Na pasta do projeto, usando PowerShell:

```powershell
go mod download
$env:DATABASE_URL = 'postgres://study:study@localhost:5432/study?sslmode=disable'
$env:ADDR = ':8080'
go run ./cmd/api
```

As credenciais acima são ilustrativas. `ADDR` é opcional; o endereço padrão é `:8080`.

Exemplos de rotas para explorar:

| Método | Rota | Objetivo |
|---|---|---|
| GET | `/api/catalog` | Consultar as definições de atributos e fatores. |
| GET | `/api/attributes/{key}/options?month=` | Consultar opções de um campo. |
| POST | `/api/employees/search` | Consultar registros com filtros e paginação. |
| POST | `/api/filters/validate` | Validar um filtro sem executar a consulta de dados. |
| POST | `/internal/catalog/reload` | Invalidar o cache do catálogo. |

O exemplo lê a identidade pelo header `X-User-Login`, que representa a parte do e-mail anterior ao `@`. Esse mecanismo é um placeholder para estudar o fluxo e não implementa autenticação real.

## Testes

Executar os testes sem os cenários de integração:

```powershell
go test ./... -skip '^TestIntegration' -v
```

Os testes de integração usam `TEST_DATABASE_URL` e exigem um banco de teste com schema e dados previamente preparados:

```powershell
$env:TEST_DATABASE_URL = 'postgres://study:study@localhost:5432/study_test?sslmode=disable'
go test ./internal/analytics -run '^TestIntegration' -v
```

Sem `TEST_DATABASE_URL`, os testes de integração são ignorados.

## Uso como referência

Este é um laboratório de estudo. O código conserva o domínio, as rotas e o nome interno do módulo do exemplo para preservar a implementação. A apresentação e o nome da pasta identificam o projeto como uma referência sobre dados dinâmicos e cache.
