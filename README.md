# Docktor — versão oficial em português do Brasil

Esta é a versão oficial em português do Brasil do [Docktor](https://github.com/marcospjr07/docktor), uma CLI somente leitura para diagnósticos de integridade de servidores Linux. `docktor scan` apresenta os indicadores do host como texto no terminal por padrão. Use `docktor scan --json` para obter uma saída legível por máquinas em automações.

## Exemplo

```text
$ docktor scan
✓ PASS Sistema operacional: Example Linux 1.0
✓ PASS Tempo ativo: 2d 3h 14min
! WARN Memória: 6.5 / 8.0 GiB em uso (81.2%)
✓ PASS Disco raiz: 42.0 / 100.0 GiB em uso (42.0%)
✓ PASS Systemd: nenhuma unidade de serviço com falha
! WARN SSH: login de root ativado; autenticação por senha ativada
! WARN Firewall: nenhuma ferramenta de firewall compatível encontrada
! WARN Pacotes: 12 atualizações disponíveis; atualidade dos metadados de pacotes desconhecida
✓ PASS Docker: daemon local acessível (versão 29.7.2)

Resumo: 5 aprovados, 4 avisos, 0 falhas
```

O exemplo é ilustrativo; os resultados dependem do host. O uso de memória e disco gera aviso a partir de 80% e falha a partir de 95%. Uma fonte de dados ausente ou inválida produz um aviso para que as outras verificações continuem. Varreduras concluídas retornam 0 independentemente dos achados de integridade; erros operacionais ou de escrita do relatório retornam 1, e erros de uso da CLI retornam 2.

## Saída JSON

Execute `docktor scan --json` para escrever um objeto JSON em stdout, seguido por uma quebra de linha. Erros e diagnósticos de uso são enviados a stderr. As mesmas verificações são executadas nos dois formatos, com os mesmos códigos de saída. Uma varredura JSON concluída retorna 0 mesmo quando verificações relatam `warn` ou `fail`; scripts devem inspecionar os status e o resumo para avaliar a integridade.

O exemplo abaixo usa os mesmos achados ilustrativos da saída de terminal:

```json
{
  "schema_version": 1,
  "checks": [
    {"name": "Sistema operacional", "status": "pass", "message": "Example Linux 1.0"},
    {"name": "Tempo ativo", "status": "pass", "message": "2d 3h 14min"},
    {"name": "Memória", "status": "warn", "message": "6.5 / 8.0 GiB em uso (81.2%)"},
    {"name": "Disco raiz", "status": "pass", "message": "42.0 / 100.0 GiB em uso (42.0%)"},
    {"name": "Systemd", "status": "pass", "message": "nenhuma unidade de serviço com falha"},
    {"name": "SSH", "status": "warn", "message": "login de root ativado; autenticação por senha ativada"},
    {"name": "Firewall", "status": "warn", "message": "nenhuma ferramenta de firewall compatível encontrada"},
    {"name": "Pacotes", "status": "warn", "message": "12 atualizações disponíveis; atualidade dos metadados de pacotes desconhecida"},
    {"name": "Docker", "status": "pass", "message": "daemon local acessível (versão 29.7.2)"}
  ],
  "summary": {"pass": 5, "warn": 4, "fail": 0, "total": 9}
}
```

A versão 1 do schema sempre inclui `schema_version`, `checks` e `summary`. Cada verificação possui os campos string `name`, `status` e `message`; os status são `pass`, `warn` ou `fail`. As verificações preservam a ordem da varredura, e um relatório vazio usa `checks: []`. O resumo sempre inclui os campos inteiros `pass`, `warn`, `fail` e `total`, inclusive quando a contagem é zero. As mensagens descrevem o estado observado do host e podem mudar; scripts devem usar os campos e valores de status, sem interpretar o texto de `message`. Uma alteração incompatível no schema incrementará `schema_version`.

## Filosofia somente leitura

Os diagnósticos nunca devem alterar a configuração do sistema. As verificações atuais leem `/etc/os-release` (ou `/usr/lib/os-release`), `/proc/uptime`, `/proc/meminfo`, estatísticas do sistema de arquivos raiz, arquivos de configuração do servidor SSH e unidades de serviço com falha por meio da consulta somente leitura `systemctl list-units`. A verificação de firewall executa `ufw status` ou, quando `firewalld.service` já está em execução, `firewall-cmd --state`. Primeiro ela verifica a unidade systemd, pois o acesso ao D-Bus poderia ativar um daemon firewalld parado. Ela não altera regras de firewall nem inicia serviços explicitamente. A verificação do Docker envia apenas `GET /version` à API local do Docker Engine por um socket Unix. Antes de se conectar aos sockets conhecidos do Docker gerenciados pelo systemd, ela lê o estado de `docker.socket` e `docker.service` e ignora o teste se o socket estiver ativo, mas o serviço não estiver. O Docktor não inicia nem reinicia serviços explicitamente e não envia requisições mutáveis à API do Docker. Não são necessários privilégios elevados, embora permissões de arquivos ou sockets possam limitar verificações individuais. Verificações futuras devem preservar essa regra.

A verificação de pacotes executa `apt-get -s -o Dir::Cache::pkgcache= -o Dir::Cache::srcpkgcache= dist-upgrade` com `LC_ALL=C`. A simulação desativa bloqueios, e as opções de cache desativam a geração de cache persistente. O Docktor nunca atualiza índices de pacotes, baixa pacotes ou realiza instalação ou remoção. Ele limita a saída capturada e não executa um shell.

## Escopo atual

- Identificação do sistema operacional Linux por `os-release`.
- Tempo ativo obtido do procfs.
- Utilização de memória com `MemTotal` e `MemAvailable` do procfs.
- Utilização do sistema de arquivos raiz por `statfs`, contabilizando blocos reservados no percentual disponível para um usuário comum.
- Unidades de serviço do systemd com falha.
- Configurações `PermitRootLogin` e `PasswordAuthentication` do servidor SSH em `sshd_config` e seus Includes.
- Estado ativo de UFW e firewalld, quando suas ferramentas de linha de comando estão disponíveis.
- Atualizações APT disponíveis com base nos índices locais de pacotes, incluindo um aviso conservador sobre a atualidade dos metadados.
- Acessibilidade do daemon local do Docker e disponibilidade da CLI do Docker.
- Saída de terminal padrão e JSON opcional com schema versão 1 e contagens `pass`, `warn`, `fail` e `total` no JSON.

A versão inicial usa apenas a biblioteca padrão do Go. A verificação de SSH lê o arquivo padrão `/etc/ssh/sshd_config` e os Includes compatíveis. Ela relata apenas `PermitRootLogin` e `PasswordAuthentication`; não verifica sobrescritas da linha de comando de um daemon em execução, não valida toda a configuração SSH nem avalia outros métodos de autenticação, como keyboard-interactive. Configurações ausentes, Includes ilegíveis ou incompatíveis e políticas que podem variar conforme `Match` retornam um aviso. A verificação de firewall aceita apenas UFW e firewalld. Ela informa se uma ferramenta compatível está ativa, não se as regras protegem uma interface ou porta específica; regras nftables e iptables brutas não são interpretadas. Se não for possível determinar o estado do firewalld no systemd, ela avisa sem contatar o firewalld. A verificação do Docker aceita um endpoint local `unix://` explícito em `DOCKER_HOST`; caso contrário, usa um socket existente em `$XDG_RUNTIME_DIR/docker.sock` ou recorre a `/var/run/docker.sock`. Valores remotos de `DOCKER_HOST` e `DOCKER_CONTEXT` são ignorados. A CLI do Docker é procurada no path, mas nunca executada; a ausência da CLI não altera o resultado do daemon. Para sockets padrão do sistema e o socket rootless selecionado, um estado systemd indisponível produz aviso sem realizar o teste. Nenhuma verificação altera configurações.

Inicialmente, a verificação Pacotes oferece suporte apenas ao APT. A contagem vem do campo **upgraded** do resumo da simulação; novas dependências e remoções não são somadas. Outros gerenciadores de pacotes retornam um aviso. A contagem reflete os índices locais e a política de seleção APT do host, não todas as versões disponíveis no upstream.

Zero atualizações só poderia justificar PASS se a atualidade dos metadados fosse estabelecida de forma confiável. Esta versão retorna WARN deliberadamente até para zero atualizações: nem `/var/lib/apt/periodic/update-stamp` nem um hook de atualização bem-sucedida provam que todos os repositórios foram atualizados. Um registro periódico com mais de 24 horas gera um aviso de registro antigo; atualizações manuais podem ter ocorrido sem atualizar esse registro. Um registro recente, ausente, ilegível ou incoerente deixa a atualidade desconhecida. A data de um único InRelease nunca é usada como prova. Nenhum marcador de atualização completa é considerado confiável neste backend inicial.

## Compilar e desenvolver

Requer Linux e Go 1.27.

```sh
go run ./cmd/docktor scan
go run ./cmd/docktor scan --json
go build -o bin/docktor ./cmd/docktor
make fmt
make vet
make test
make build
make check
```

`docktor` sem argumentos e `docktor --help` exibem a ajuda. `docktor scan --help` exibe a ajuda do comando. `make check` verifica a formatação, executa o vet e os testes e compila a CLI.

## Versão em inglês

Novas funcionalidades nascem no [repositório upstream em inglês](https://github.com/marcospjr07/docktor) e são sincronizadas para esta edição oficial em português do Brasil. A edição PT-BR preserva a arquitetura, o comportamento e o contrato JSON estrutural do upstream.

## Roteiro

Adicionar verificações independentes e somente leitura para rede. Outros gerenciadores de pacotes, um registro confiável de atualização completa dos índices e uma interpretação mais ampla das regras de firewall podem seguir em projetos separados.
