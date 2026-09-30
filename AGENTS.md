# Diretrizes de arquitetura e contribuição

- `cmd/docktor` é responsável pelo tratamento dos argumentos da CLI e pelos códigos de saída. Varreduras concluídas retornam 0 independentemente dos resultados das verificações; erros operacionais ou de escrita do relatório retornam 1, e erros de uso da CLI retornam 2.
- `internal/check` define `Status`, `Result`, `Check`, o executor sequencial das verificações e as contagens do resumo. Mantenha esse pacote livre de detalhes do Linux e do terminal.
- `internal/linux` contém verificações do Linux em modo somente leitura. `internal/reporter` formata o relatório resultante. Adicione uma nova verificação por meio de `linux.Checks()`, sem acoplá-la ao formatador de relatórios.
- As verificações podem ler dados do servidor e consultar APIs do sistema em modo somente leitura. Elas não devem escrever arquivos, alterar configurações, reiniciar serviços nem invocar comandos com efeitos colaterais.
- Propague `context.Context` pelo executor das verificações. Trate dados de sistema indisponíveis ou malformados como avisos e prossiga com as demais verificações. Injete pequenas funções de leitura de arquivos ou de consulta de metadados (`stat`) nos testes, em vez de depender do sistema em que os testes são executados.
- Mantenha a base inicial restrita à biblioteca padrão. Adicione dependências apenas como uma decisão explícita do projeto.
- Execute `make check` com Go 1.27 antes de integrar alterações. Mantenha os testes focados em análise de dados de entrada, cálculos, relatórios e comportamento da CLI.
