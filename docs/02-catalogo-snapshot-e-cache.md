# 02 — Catálogo, snapshot e cache

Código: [catalog.go](../internal/analytics/catalog.go). As instâncias são montadas em [main.go](../cmd/api/main.go).

## 1. Separe três coisas antes de começar

| Peça | O que representa |
|---|---|
| `attribute_definitions` e `risk_factors` | Definições persistidas no banco. |
| `Snapshot` | Uma versão dessas definições, já carregada e indexada na memória da API. |
| `Catalog` | O objeto que guarda o snapshot atual e controla sua validade e recarga. |

Uma definição responde “o que este campo significa para a aplicação?”. O valor de um funcionário responde “quanto este funcionário tem nesse campo?”. O cache deste capítulo guarda a primeira informação.

Por exemplo, a definição pode dizer que `salario_atual` é um número com unidade monetária. O valor `4500` de um funcionário não fica no cache do catálogo.

Também não confunda `Snapshot` com `employee_snapshots`: o primeiro é um objeto Go; o segundo é uma tabela de dados mensais.

## 2. As duas structs que representam definições

`AttributeDef` contém `Key`, `DataType`, `Storage`, `Unit`, `Filterable`, `SimpleFilter` e `DisplayOrder`.

`RiskFactor` contém `Key`, `ValueType`, `Unit`, `Visible` e `DisplayOrder`.

O tipo `*string` de `Unit` permite distinguir uma unidade presente de uma unidade ausente, representada por `nil`. Os booleanos carregam regras diferentes: `Filterable` participa da validação dos filtros; `Visible` participa de algumas respostas de fatores. Receber o catálogo não significa que todas essas regras já foram aplicadas.

## 3. Como o snapshot organiza as definições

```go
type Snapshot struct {
    Attributes []AttributeDef
    Factors    []RiskFactor

    attrByKey   map[string]AttributeDef
    factorByKey map[string]RiskFactor
}
```

Existem duas coleções e dois índices:

| Campo | Uso |
|---|---|
| `Attributes` | Percorrer ou serializar os atributos na ordem carregada. |
| `Factors` | Percorrer ou serializar os fatores na ordem carregada. |
| `attrByKey` | Encontrar um atributo por sua chave. |
| `factorByKey` | Encontrar um fator por sua chave. |

Não existe a regra “toda struct precisa de um mapa”. Aqui cada coleção ganha um índice porque o código precisa localizar seus itens por chave repetidamente.

Um índice pode ser entendido como o índice de um livro: ele permite localizar um assunto sem ler todas as páginas. Manter esse índice consome alguma memória e exige preparação, mas evita repetir a busca sequencial.

## 4. Por que usar map, além do slice

Sem mapa, a busca seria parecida com isto:

```go
for _, def := range attrs {
    if def.Key == key {
        return def, true
    }
}
```

No pior caso, esse laço percorre todos os itens: uma busca linear, descrita como `O(n)`.

Com mapa, escrevemos:

```go
def, ok := byKey[key]
```

O runtime usa uma estrutura de hash para localizar a chave. A busca tem custo médio esperado próximo de `O(1)`: em vez de percorrer deliberadamente toda a lista, consulta o índice. Isso não é uma garantia de tempo fixo em qualquer situação nem elimina todo custo da operação.

Para listas pequenas, o ganho absoluto pode ser modesto. Aqui o mapa também comunica a intenção e simplifica os pontos que consultam definições durante a montagem de filtros.

A ordem de percurso de um mapa Go não é garantida. Por isso as listas continuam úteis para preservar a ordem de exibição. Veja também a [explicação de mapas em Effective Go](https://go.dev/doc/effective_go#maps).

## 5. O que NewSnapshot faz, linha por linha

O construtor real começa assim:

```go
s := &Snapshot{
    Attributes:  attrs,
    Factors:     factors,
    attrByKey:   make(map[string]AttributeDef, len(attrs)),
    factorByKey: make(map[string]RiskFactor, len(factors)),
}
```

1. `&Snapshot{...}` prepara uma struct e obtém um ponteiro para ela.
2. `Attributes: attrs` guarda o slice de atributos recebido.
3. `Factors: factors` guarda o slice de fatores recebido.
4. Os dois `make` criam mapas inicialmente vazios.
5. O número passado a `make(map..., n)` é uma sugestão de tamanho inicial. Ele não insere `n` definições.

Depois os laços preenchem os mapas:

```go
for _, a := range attrs {
    s.attrByKey[a.Key] = a
}
for _, f := range factors {
    s.factorByKey[f.Key] = f
}
```

Em cada iteração, a chave vem do próprio item. Exemplo:

```text
attrs[0] = { Key: "department", ... }
attrs[1] = { Key: "salario_atual", ... }

attrByKey["department"]    = valor da primeira definição
attrByKey["salario_atual"] = valor da segunda definição
```

Um detalhe: o mapa é `map[string]AttributeDef`, não `map[string]*AttributeDef`. A atribuição guarda uma cópia do valor da struct; não guarda um ponteiro para a posição do slice.

Essa cópia não é profunda: um campo ponteiro, como `Unit`, ainda pode apontar para o mesmo valor. Os slices recebidos também não são copiados profundamente pelo construtor. Portanto a implementação depende de ninguém modificar esses dados depois da publicação do snapshot.

Se duas definições chegassem com a mesma chave, a última atribuição substituiria a anterior no mapa. O construtor não valida duplicatas; o exemplo espera um catálogo coerente.

## 6. Por que há métodos Attribute e Factor

```go
func (s *Snapshot) Attribute(key string) (AttributeDef, bool) {
    a, ok := s.attrByKey[key]
    return a, ok
}
```

`Factor` faz a mesma operação no outro mapa. Assim quem usa o snapshot não precisa conhecer o nome do índice interno.

Exemplo conceitual dentro do pacote:

```go
def, found := snap.Attribute("salario_atual")
if !found {
    // O catálogo não conhece a chave.
}
```

Esses métodos não consultam o banco nem verificam o TTL. Eles apenas leem o snapshot que já receberam.

Os mapas têm nomes não exportados. Ao enviar o snapshot como JSON, o serializador usa os campos exportados com as tags do arquivo original: `attributes` e `factors`.

## 7. Quem mantém o snapshot na memória

```go
type Catalog struct {
    db       *pgxpool.Pool
    ttl      time.Duration
    mu       sync.Mutex
    snap     *Snapshot
    loadedAt time.Time
}
```

| Campo | Função |
|---|---|
| `db` | Referência ao pool usado para consultar as definições. |
| `ttl` | Por quanto tempo uma carga bem-sucedida é considerada atual. |
| `mu` | Protege o acesso ao estado compartilhado do catálogo. |
| `snap` | Ponteiro para a versão atualmente guardada. |
| `loadedAt` | Horário de publicação da última carga bem-sucedida, ou horário zerado pela invalidação. |

`Catalog` é criado em `main` e compartilhado com o repository e o servidor. Enquanto essa instância estiver acessível, ela mantém a referência ao snapshot atual. A memória pertence ao processo da API; não é Redis, arquivo ou armazenamento no banco.

## 8. NewCatalog prepara, mas não carrega

```go
return &Catalog{db: db, ttl: ttl}
```

Os outros campos ficam com os valores zero de Go: `snap == nil`, horário zerado e mutex desbloqueado. A primeira chamada a `Get` fará a carga.

`NewStaticCatalog` é uma alternativa para uso sem banco: recebe um snapshot pronto e usa uma duração muito longa. Não é a forma usada em `main`.

## 9. TTL é a idade permitida da carga

No programa, `time.Minute` configura um minuto. A decisão está aqui:

```go
if c.snap != nil && time.Since(c.loadedAt) < c.ttl {
    return c.snap, nil
}
```

A leitura é: “se já há um snapshot e a idade dele é menor que o TTL, devolva essa versão”.

| Momento ilustrativo | Resultado |
|---|---|
| 10:00:00, primeira chamada | Carrega e registra `loadedAt`. |
| 10:00:20 | Reutiliza a mesma versão. |
| 10:00:50 | Reutiliza; o acesso não renova a validade. |
| 10:01:00 ou depois, próxima chamada | Tenta carregar outra versão. |
| Nenhuma chamada por uma hora | Nada é atualizado automaticamente nesse intervalo. |

O TTL não apaga objetos, não chama o garbage collector e não inicia um temporizador. É uma condição verificada por `Get`. Esse comportamento é carregamento e atualização sob demanda.

## 10. A sequência completa de Get

1. Adquire o mutex.
2. Agenda o `Unlock` com `defer`.
3. Se houver snapshot válido, devolve-o.
4. Caso contrário, chama `loadSnapshot(ctx, c.db)`.
5. Se a carga falhar e houver versão anterior, devolve a anterior.
6. Se falhar sem versão anterior, devolve o erro.
7. Se der certo, substitui `snap` e registra o horário atual.
8. Devolve a nova versão e libera o mutex ao sair.

A atribuição que publica a nova versão é:

```go
c.snap, c.loadedAt = snap, time.Now()
```

O horário é atualizado depois da carga bem-sucedida. Uma falha não transforma o snapshot antigo em uma carga recente.

## 11. O caminho do banco até o snapshot

`loadSnapshot` faz duas consultas sequenciais:

1. Lê `attribute_definitions`, ordenando por `display_order, key`.
2. Converte cada linha em `AttributeDef` com `Scan`.
3. Lê `risk_factors` com a mesma regra de ordenação.
4. Converte cada linha em `RiskFactor`.
5. Chama `NewSnapshot(attrs, factors)`.
6. Retorna somente depois que as duas coleções foram carregadas.

`pgx.CollectRows` reúne as linhas e fecha o resultado. Se alguma consulta ou leitura falhar, a nova versão não é publicada pela função `Get`.

“Snapshot” aqui significa uma versão estável em memória depois de pronta. As duas consultas não estão dentro de uma transação explícita; o nome não garante uma fotografia transacional simultânea das duas tabelas.

## 12. Mutex: organizar chamadas concorrentes

Um servidor atende requisições simultaneamente. Sem coordenação, duas chamadas poderiam observar uma versão vencida e recarregar ao mesmo tempo.

```go
c.mu.Lock()
defer c.mu.Unlock()
```

O mutex deixa uma chamada por vez acessar a região protegida. Segundo a [documentação de sync.Mutex](https://pkg.go.dev/sync#Mutex.Lock), uma chamada a `Lock` espera quando o mutex já está ocupado.

Neste exemplo, o lock cobre inclusive as consultas de recarga:

```text
Requisição A: Lock → carrega do banco → publica S2 → Unlock
Requisição B:        aguarda......................→ Lock → recebe S2
```

Mesmo um acerto no cache passa por `Lock` e `Unlock`; não é uma implementação sem locks. Porém, depois que `Get` retorna, a requisição percorre o snapshot sem segurar esse mutex.

Isso funciona porque o snapshot publicado é tratado como somente leitura. O mutex de `Catalog` não protege alguém que decida modificar diretamente os slices exportados de um snapshot.

## 13. Trocar o ponteiro não modifica o objeto antigo

```text
Antes:
Catalog.snap ───→ S1
Requisição A ───→ S1

Depois da recarga:
Catalog.snap ───→ S2
Requisição A ───→ S1
Requisição B ───→ S2
```

A requisição A continua usando a versão que já recebeu. A atribuição não faz seu ponteiro mudar automaticamente para S2.

Quando não houver mais referências alcançáveis a S1, o coletor de lixo poderá recuperar sua memória. O momento dessa recuperação não é imediato nem determinado pelo TTL.

## 14. Invalidate antecipa a próxima recarga

```go
func (c *Catalog) Invalidate() {
    c.mu.Lock()
    c.loadedAt = time.Time{}
    c.mu.Unlock()
}
```

`time.Time{}` é o horário zero. Com ele, a verificação de idade deixa de considerar o snapshot recente.

Fluxo de exemplo:

1. Um processamento externo atualiza as definições no banco.
2. Esse processamento chama `POST /internal/catalog/reload`.
3. O handler executa `Invalidate` e responde 204.
4. A próxima chamada a `Get` tenta recarregar.
5. Se der certo, publica a nova versão.

Essa rota não executa a consulta naquele momento. A integração que a chama depois de um processamento também não está implementada aqui: o exemplo apenas disponibiliza o handler.

`Invalidate` mantém o ponteiro anterior. Se a recarga seguinte falhar, o fallback ainda consegue usar aquela versão. Se uma recarga estiver em andamento, a invalidação aguarda o mesmo mutex.

## 15. O comportamento quando a carga falha

| Situação | Resultado |
|---|---|
| Primeira carga falha | Erro para quem pediu o catálogo. |
| Recarga falha com snapshot anterior | Retorna a versão anterior, sem devolver esse erro ao chamador. |
| Próxima chamada depois da falha | Tenta recarregar novamente; não há intervalo de espera adicional. |

Esse reaproveitamento de uma versão vencida em caso de erro é frequentemente chamado de `stale-if-error`. Por isso um TTL de um minuto não garante defasagem máxima de um minuto durante falhas.

Cada instância da API tem seu próprio cache. Invalidar uma delas não envia um aviso às outras. Reiniciar o processo também perde o cache local.

## 16. O que levar deste capítulo

O snapshot organiza uma versão dos metadados. Os mapas aceleram consultas por chave; os slices preservam a ordem. O catálogo decide quando reutilizar ou substituir essa versão. O TTL mede a idade, o mutex coordena a troca e `Invalidate` força uma tentativa de recarga na próxima utilização.

Perguntas para conferir:

1. Buscar um atributo em `snap.Attribute` acessa o banco? Não.
2. Passar do TTL libera a memória? Não.
3. Trocar `Catalog.snap` altera o snapshot usado por uma requisição antiga? Não.
4. `make(map..., len(attrs))` já contém os atributos? Não; os laços os inserem.
5. O construtor faz cópias profundas? Não; a disciplina de somente leitura é necessária.

[Anterior: fundamentos](01-fundamentos-e-inicializacao.md) · [Índice](README.md) · [Próximo: campos dinâmicos e SQL](03-campos-dinamicos-e-sql.md)
