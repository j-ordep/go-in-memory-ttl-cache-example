# 07 — Testes e exercícios de leitura

Código: [filter_test.go](../internal/analytics/filter_test.go) e [repository_integration_test.go](../internal/analytics/repository_integration_test.go).

## 1. Teste unitário e teste de integração

Um teste unitário pode conferir uma regra sem depender de um serviço externo. Aqui os testes de filtro criam definições em memória e verificam a compilação.

Um teste de integração usa o PostgreSQL para conferir consultas e resultados reais. Ele depende do schema e dos dados esperados.

Passar nos testes unitários não demonstra que o schema externo existe, que os casts aceitam todos os dados de um produtor ou que uma consulta terá determinado desempenho.

## 2. Como o teste monta um catálogo sem banco

`testCatalog` chama `NewSnapshot` com uma lista de definições criada no próprio teste. O snapshot recebe, portanto, as mesmas listas e índices usados na aplicação, mas não precisa de `loadSnapshot`.

O helper `compile`:

1. Interpreta um JSON em `FilterNode`.
2. Cria `args`.
3. Chama `compileFilter` com o snapshot preparado.
4. Devolve SQL, argumentos e erro.

Observe a separação: o teste consegue verificar o compilador sem iniciar HTTP e sem abrir conexão.

## 3. O que os testes existentes mostram

| Teste | O que observar |
|---|---|
| `TestCompileNestedFilter` | Parênteses dos grupos, tipos das expressões e ordem dos parâmetros. |
| `TestKeysNeverReachSQL` | A chave JSON e o valor da condição seguem como argumentos em vez de aparecerem na string SQL. |
| `TestRejectsInvalidFilters` | Casos de campo inexistente, flag de filtro, coluna fora da lista permitida, operador, valor, intervalo, grupo e profundidade. |
| `TestBooleanDateAndNulls` | Exemplos aceitos de booleano, data e operadores de ausência ou exclusão. |

Não leia o nome do último teste como uma prova de que qualquer JSON `null` é rejeitado em comparações. Os casos concretos são a evidência do que foi exercitado.

Também não há, nesses arquivos, uma bateria específica de testes de TTL, concorrência do cache ou invalidação. As explicações do capítulo 02 acompanham a implementação; não são um relatório de testes de cache executados.

## 4. Como estudar a comparação entre SQL e argumentos

Em `TestCompileNestedFilter`, anote:

1. Quais campos são colunas permitidas.
2. Qual campo vem de JSONB.
3. Em qual ordem o compilador visita os filhos.
4. Quando `fieldExpr` precisa adicionar uma chave como argumento.
5. Quando a condição adiciona o valor comparado.

O número de argumentos não precisa coincidir com o número de condições: uma condição dinâmica pode usar um argumento para a chave e outro para o valor.

## 5. O que os testes de integração exigem

`integrationRepo` lê `TEST_DATABASE_URL`. Sem essa variável, chama `t.Skip`.

Quando a variável está preenchida, cria um pool e um catálogo real, e registra o fechamento do pool com `t.Cleanup`.

Os testes esperam dados específicos: quantidades conhecidas de definições, meses, funcionários, fatores e observações. Um PostgreSQL vazio, ou um banco apenas com tabelas de nomes semelhantes, não atende esses requisitos.

O repositório não inclui os scripts de schema e dados citados nos comentários originais. Por isso é possível estudar os testes, mas não reproduzir seus resultados de integração apenas clonando o código e subindo um banco vazio.

O login chamado `admin` no fixture não introduz uma regra especial de administrador na implementação.

## 6. Comandos de referência

Para executar os testes sem os cenários de integração:

```powershell
go test ./... -skip '^TestIntegration' -v
```

Isso evita executar os testes de integração mesmo que a variável de conexão já esteja definida no ambiente.

Somente depois de preparar um banco de teste local compatível:

```powershell
$env:TEST_DATABASE_URL = 'postgres://study:study@localhost:5432/study_test?sslmode=disable'
go test ./internal/analytics -run '^TestIntegration' -v
```

As credenciais são ilustrativas. Esses comandos explicam como o projeto é exercitado; a criação desta documentação não executa consultas em bancos.

## 7. Exercício: seguir um lookup

Sem alterar código, use duas definições imaginárias: `department` e `salario_atual`.

1. Desenhe o slice com duas posições.
2. Desenhe o conteúdo de `attrByKey` após `NewSnapshot`.
3. Mostre o retorno de `Attribute("salario_atual")`.
4. Mostre o retorno para uma chave ausente.
5. Explique por que o booleano do retorno é necessário.

Resposta esperada: os dois itens ficam indexados por sua chave; a chave conhecida devolve valor e `true`; a desconhecida devolve valor zero e `false`. O mapa guarda structs por valor, não ponteiros para posições do slice.

## 8. Exercício: seguir a vida de duas versões

Imagine TTL de um minuto:

- 10:00:00: primeira carga bem-sucedida, versão S1.
- 10:00:30: uma requisição recebe S1.
- 10:00:40: o catálogo é invalidado.
- 10:00:41: outra chamada recarrega e publica S2.

Perguntas:

1. A requisição iniciada antes da invalidação precisa mudar para S2?
2. O que aconteceria se a carga de 10:00:41 falhasse?
3. O TTL apagou S1?
4. Quando a memória de S1 pode ser recuperada?

Resposta esperada: a requisição antiga pode manter S1; uma falha permite retornar S1; o TTL não apaga objetos; o coletor pode recuperar a memória quando não houver referências alcançáveis.

## 9. Exercício: seguir a consulta

Use o filtro `tenure_months > 12`, com mês e login explícitos.

1. Mês ocupa qual posição?
2. Login ocupa qual posição?
3. Sendo uma coluna permitida, o nome do campo precisa virar argumento?
4. O número 12 ocupa qual posição?

Resposta esperada: mês em `$1`, login em `$2`; a expressão da coluna vem do mapa permitido e o valor 12 ocupa `$3`.

Agora repita com `salario_atual > 4500`, supondo armazenamento em atributos. A chave ocupa `$3` e o número ocupa `$4`.

## 10. Exercício: distinguir comportamentos

Explique estas frases com suas palavras:

- “O catálogo está em cache” não significa que a busca de funcionários dispensa banco.
- “O filtro foi validado” não significa que ele foi salvo.
- “O snapshot é estável” não significa que há cópia profunda ou uma transação única.
- “A chave existe no catálogo” não significa que todas as operações aplicam os mesmos flags.
- “O TTL venceu” não significa que um temporizador já fez a recarga.

Se conseguir apontar o arquivo e a função responsáveis por cada frase, você já sabe usar este projeto como referência.

## 11. Cuidados com os arquivos de estudo

Salve os Markdown em UTF-8 e preserve as quebras de linha. Anexar texto com outra codificação pode introduzir caracteres nulos e prejudicar a exibição no GitHub.

Ao adaptar o exemplo no futuro, escolha um fluxo pequeno e revise suas entradas, saídas e casos de falha. Os exercícios deste capítulo não exigem modificar o código nem criar novas funcionalidades.

[Anterior: HTTP](06-http-e-fluxo-completo.md) · [Índice](README.md) · [Consultar o glossário](08-glossario.md)
