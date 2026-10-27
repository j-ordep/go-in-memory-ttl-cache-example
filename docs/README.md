# Trilha de aprendizado

Esta documentação explica o exemplo para quem ainda está aprendendo Go, APIs, PostgreSQL e cache. Você pode acompanhar a maior parte da trilha apenas lendo o código; não precisa conectar a um banco para entender o fluxo.

Comece pelo [mapa de leitura e localização dos arquivos](00-mapa-de-leitura.md). Ele indica o que abrir, em qual ordem e quais funções procurar.

## Ordem sugerida

| Etapa | Leitura | O que você vai entender |
|---|---|---|
| 0 | [Mapa do projeto](00-mapa-de-leitura.md) | Onde estão as peças e como navegar entre elas. |
| 1 | [Fundamentos e inicialização](01-fundamentos-e-inicializacao.md) | Structs, slices, mapas, ponteiros, construtores e criação dos objetos. |
| 2 | [Catálogo, snapshot e cache](02-catalogo-snapshot-e-cache.md) | Listas, índices por chave, TTL, mutex, troca de versão e invalidação. |
| 3 | [Campos dinâmicos e parâmetros SQL](03-campos-dinamicos-e-sql.md) | Como um nome de campo vira uma expressão SQL com o tipo correto. |
| 4 | [Filtros passo a passo](04-filtros-passo-a-passo.md) | Como uma árvore JSON vira condições SQL e argumentos separados. |
| 5 | [Repository e consultas](05-repository-e-consultas.md) | Recorte por mês, visibilidade, listagem, agrupamentos e leitura das linhas. |
| 6 | [HTTP e um fluxo completo](06-http-e-fluxo-completo.md) | Como a requisição entra, atravessa as camadas e vira uma resposta. |
| 7 | [Testes e exercícios](07-testes-e-exercicios.md) | Como ler os testes e conferir o aprendizado sem alterar o projeto. |

O [glossário](08-glossario.md) pode ficar aberto durante toda a leitura.

## Como usar a trilha

1. Leia um capítulo e abra o arquivo de código indicado no início.
2. Localize os nomes das funções com a busca do editor.
3. Acompanhe um exemplo pequeno, anotando os valores das variáveis.
4. Responda às perguntas no final antes de seguir para o próximo capítulo.
5. Se quiser apenas lembrar uma parte, use o mapa em vez de recomeçar toda a trilha.

Os trechos de código destacados podem estar reduzidos para mostrar uma ideia. O arquivo Go indicado é a referência para a implementação completa. Exemplos de dados e JSON são ilustrativos: não significam que essas definições já existem em algum banco.

## O que esta documentação descreve

A trilha descreve a implementação que está neste repositório. Ela diferencia o comportamento existente de possibilidades de evolução. Em particular:

- o cache guarda metadados do catálogo, não as páginas de funcionários;
- o nome `Snapshot` significa uma versão do catálogo em memória;
- a tabela `employee_snapshots` guarda dados de funcionários por mês e é outra coisa;
- o exemplo depende de schema e dados externos que não estão incluídos;
- o cabeçalho usado como identidade é uma simplificação do exemplo;
- as limitações observadas no código são explicadas junto do fluxo correspondente.

[Voltar ao README do projeto](../README.md) · [Começar pelo mapa](00-mapa-de-leitura.md)
