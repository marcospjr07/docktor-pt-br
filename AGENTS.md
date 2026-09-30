# Diretrizes de arquitetura e contribuição

- `cmd/docktor` é responsável pelo tratamento dos argumentos da CLI e pelos códigos de saída. Varreduras concluídas retornam 0 independentemente dos achados de integridade; erros operacionais ou de escrita do relatório retornam 1, e erros de uso da CLI retornam 2.
- `internal/check` define `Status`, `Result`, `Check`, o runner ordenado e as contagens do resumo. Mantenha esse pacote livre de detalhes do Linux e do terminal.
- `internal/linux` contém verificações somente leitura do Linux. `internal/reporter` formata o relatório resultante. Adicione uma nova verificação por meio de `linux.Checks()`, sem acoplá-la ao reporter.
- As verificações podem ler dados do host e consultar APIs de sistema somente leitura. Elas não devem escrever arquivos, alterar configurações, reiniciar serviços nem invocar comandos com efeitos colaterais.
- Propague `context.Context` pelo runner. Trate dados de sistema indisponíveis ou malformados como aviso e continue as outras verificações. Injete pequenas funções de leitura de arquivo ou stat nos testes, em vez de depender do host.
- Mantenha a base inicial somente com a biblioteca padrão. Adicione dependências apenas como uma decisão explícita do projeto.
- Execute `make check` com Go 1.27 antes de integrar alterações. Mantenha os testes focados em parsing, cálculos, relatórios e comportamento da CLI.
