# hwinfo-mcp

[English](README.md) · **Português (Brasil)** · [Español](README.es.md)

Pergunte a um assistente de IA sobre o hardware do seu PC e deixe que ele leia
os sensores de verdade.

O hwinfo-mcp é um servidor [Model Context Protocol](https://modelcontextprotocol.io)
somente leitura. Ele dá a um assistente como o Claude acesso a tudo o que o
[HWiNFO](https://www.hwinfo.com) monitora em um PC com Windows: temperaturas,
tensões, rotação das ventoinhas, consumo de energia, frequências, uso, saúde
dos discos e contadores de erros de hardware.

Isso faz dele uma ferramenta para encontrar problemas de hardware. Em vez de
percorrer centenas de linhas no HWiNFO, você pergunta:

> Minha CPU está superaquecendo? Veja a temperatura atual, o pico e se ela
> chegou a limitar o desempenho.

O assistente lê os sensores, compara os valores atuais com os picos que o
HWiNFO registrou e diz o que encontrou.

É um único executável que fala MCP por stdin/stdout. Ele lê o bloco de memória
que o HWiNFO compartilha com outros programas e nunca escreve nele, não abre
conexão de rede e não altera nada no sistema.

**Somente Windows.** O HWiNFO só existe para Windows, e a memória compartilhada
que este servidor lê também, por isso o executável é publicado para Windows em
x64 e em Arm. O plugin e os pacotes trazem a versão x64, e nada foi testado no
Windows em Arm. O Linux dentro do WSL 2 tampouco enxerga os chips de sensores do
PC: execute o servidor do lado do Windows.

## Início rápido

São três passos: instalar o servidor, pôr o HWiNFO para publicar, e perguntar.

### 1. Instale o pacote ou o plugin

Há cinco formas de instalar o servidor. Da mais fácil à mais trabalhosa:

| Forma | Para | O que exige |
|---|---|---|
| O pacote com o HWiNFO | Claude Desktop, em um PC sem HWiNFO | Baixar um arquivo e abri-lo. O HWiNFO vem dentro. |
| O pacote | Claude Desktop | Baixar um arquivo e abri-lo. |
| O plugin | Claude Code | Um comando. |
| [O executável](#instalando-o-servidor-manualmente) | Qualquer cliente MCP | Baixá-lo e registrá-lo no cliente por conta própria. |
| [Sua própria compilação](#compilando-a-partir-do-código-fonte) | Qualquer cliente MCP | O mesmo, depois de compilar com Go. |

**Claude Desktop: um pacote.** Dois arquivos instalam a mesma extensão. Use o
[`hwinfo-mcp-with-hwinfo.mcpb`](https://github.com/templarfelix/hwinfo-mcp/releases/latest/download/hwinfo-mcp-with-hwinfo.mcpb)
(24 MB) se o PC ainda não tem o HWiNFO: ele traz também
[o ZIP portátil do HWiNFO](#a-cópia-do-hwinfo-no-pacote). Use o
[`hwinfo-mcp.mcpb`](https://github.com/templarfelix/hwinfo-mcp/releases/latest/download/hwinfo-mcp.mcpb)
(4 MB) se o HWiNFO já está instalado, ou se você prefere instalá-lo por conta
própria. Abra o arquivo, ou arraste-o para o Claude Desktop. Nenhum dos dois
pacotes é assinado. Se não instalar,
[instale o servidor manualmente](#instalando-o-servidor-manualmente) e, por
favor, abra uma issue.

**Claude Code: o plugin.** Um comando o instala:

```
claude plugin install hwinfo --marketplace templarfelix/hwinfo-mcp
```

Ele exige o Claude Code 2.1.292 ou mais recente. Em uma versão mais antiga, use
estes dois:

```
claude plugin marketplace add templarfelix/hwinfo-mcp
claude plugin install hwinfo@hwinfo-mcp
```

O plugin traz o servidor junto com quatro
[skills](#skills-que-vêm-com-o-plugin) que dão ao assistente um método para ler
os sensores, então não há executável para baixar. Dentro de uma sessão (versão
2.1.275 ou mais recente),
`/plugin install hwinfo --marketplace templarfelix/hwinfo-mcp` adiciona o
marketplace e abre a página do plugin, onde você instala.

**Qualquer outro cliente MCP**, ou se você prefere registrar o servidor por
conta própria: veja
[Instalando o servidor manualmente](#instalando-o-servidor-manualmente).

### 2. Ponha o HWiNFO para publicar

Quem mede é o HWiNFO; este projeto só lê o que o HWiNFO publica. O HWiNFO é um
programa separado, com licença própria. Ao abrir, ele pede permissão de
administrador, necessária para acessar o hardware, e a publicação vem
desligada.

**Com o pacote que traz o HWiNFO**, pergunte ao assistente:

> O HWiNFO está publicando os dados dos sensores?

Enquanto não estiver, a resposta indica um arquivo, `Setup-HWiNFO.cmd`, na
pasta da extensão. Executá-lo é um passo seu: cole o caminho dele na caixa
Executar (Win+R). Ele descompacta o HWiNFO em uma pasta própria, põe ao lado um
arquivo de configuração que liga a publicação, e o inicia. Quando o Windows
pedir permissão de administrador, o fornecedor mostrado deve ser REALiX.
[A cópia do HWiNFO no pacote](#a-cópia-do-hwinfo-no-pacote) diz exatamente o
que o arquivo faz e no que ele não mexe.

**Caso contrário**, instale o HWiNFO com:

```
winget install REALiX.HWiNFO
```

Ou baixe o [HWiNFO64](https://www.hwinfo.com/download/). Abra-o e ligue a
publicação uma vez:

1. Na tela inicial, marque **Sensors-only** (somente sensores) e clique em
   **Settings** (configurações).
2. Na aba **General / User Interface**, ative **Shared Memory Support**. É a
   opção de que este projeto depende. Na mesma aba, ative
   **Show Sensors on Startup**, **Minimize Sensors on Startup**,
   **Minimize Main Window on Startup**, **Minimize Sensors instead of closing**
   e **Auto Start**, e desative **Show Welcome Screen and Progress**, para que
   o HWiNFO inicie com o Windows e continue publicando pela área de
   notificação. [Configurações do HWiNFO](#configurações-do-hwinfo) explica
   para que serve cada uma.
3. Confirme com **OK** e reinicie o HWiNFO: feche-o pelo ícone da área de
   notificação e abra de novo. Na máquina em que este projeto foi desenvolvido,
   as leituras só passaram a ser publicadas depois desse reinício.

Nos dois casos, a edição gratuita do HWiNFO para de publicar depois de 12 horas
de execução; veja [Configurações do HWiNFO](#configurações-do-hwinfo).

### 3. Pergunte

Comece com:

> O HWiNFO está publicando os dados dos sensores?

O assistente chama `hwinfo_status` e deve responder que os sensores estão
ativos e de quando é a última leitura. Se não estiverem, a resposta diz o que
há de errado e como corrigir; com o plugin, `/hwinfo:setup` conduz o ajuste.

Depois pergunte o que você queria saber; [Como perguntar](#como-perguntar) traz
exemplos.

## Como perguntar

Você não chama as ferramentas diretamente. Pergunte com suas palavras, em
qualquer idioma; o assistente escolhe a ferramenta e os filtros. Alguns
exemplos para começar:

- Qual é a temperatura da minha CPU e da minha GPU agora?
- Em que rotação estão as ventoinhas?
- Quanta energia o processador e a placa de vídeo estão consumindo?
- Quais sensores o HWiNFO enxerga neste PC?

### Skills que vêm com o plugin

O plugin acrescenta quatro skills. O assistente as usa sozinho quando a
pergunta pede, e você também pode chamar uma pelo nome:

| Skill | O que faz |
|---|---|
| `/hwinfo:thermal-check` | Avalia se a CPU ou a placa de vídeo esquenta demais, se está sendo contida pela temperatura ou se precisa de pasta térmica ou thermal pads novos. Trabalha com temperatura e potência lidas no mesmo instante, sob carga, e diz o que os sensores não conseguem mostrar. |
| `/hwinfo:voltage-check` | Avalia se as linhas da fonte e as entradas de energia da placa de vídeo estão na faixa, e como se comportam quando a carga muda. Começa dizendo o que os sensores de tensão da placa-mãe não conseguem mostrar. |
| `/hwinfo:hardware-checkup` | Percorre contadores de erro de hardware, saúde dos discos, tensões, ventoinhas, memória e limites, e informa o que pede atenção, o que está bem e o que não deu para verificar. |
| `/hwinfo:setup` | Faz o HWiNFO publicar as leituras, ou explica por que elas pararam. |

Sem o plugin as mesmas perguntas funcionam; o assistente fica com as
ferramentas, mas sem o método.

### Caçando problemas de hardware

| Suspeita | Pergunte algo como | O que o assistente lê |
|---|---|---|
| A CPU está superaquecendo | *Minha CPU está superaquecendo? Compare a temperatura atual com o pico e diga se houve throttling.* | A temperatura de controle da CPU (Tctl/Tdie ou do encapsulamento) com mínimo, máximo e média, as temperaturas por CCD e do núcleo mais quente, a potência do encapsulamento e os indicadores de throttling térmico. |
| O desempenho cai sob carga | *Alguma coisa está segurando minha CPU ou GPU? Veja os limites térmicos, de potência e de corrente.* | Leituras como `Thermal Limit`, `CPU PPT Limit`, `Thermal Throttling (HTC)` e, na GPU, `Performance Limit - Thermal` e `Performance Limit - Power`. |
| A placa de vídeo esquenta demais, ou a pasta e os pads térmicos estão gastos | *Compare o hot spot da GPU com a temperatura do núcleo e confira a temperatura da memória.* | Temperaturas do núcleo, do hot spot e da junção da memória da GPU, a potência da placa e a rotação das ventoinhas. |
| Uma ventoinha morreu ou trava | *Todas as ventoinhas estão girando? Mostre a rotação atual, a menor e a maior de cada uma.* | Todas as leituras de ventoinha, com as estatísticas. |
| O PC desliga ou reinicia sob carga | *Meu PC reinicia quando eu jogo. Confira as temperaturas, as linhas de 12 V, 5 V e 3,3 V e os contadores de erro de hardware.* | Temperaturas, as tensões da placa-mãe com mínimo e máximo, e a contagem de erros de hardware do Windows (WHEA). |
| Um disco está falhando | *Como está a saúde do meu SSD?* | O sensor S.M.A.R.T.: vida útil restante, reserva disponível, indicadores de falha e de aviso, temperatura. |
| Memória ou overclock instável | *Há algum sinal de problema de memória? Confira a temperatura e os indicadores dos módulos e os erros de hardware.* | Os sensores dos módulos de memória (temperatura, tensões, indicadores do PMIC) e a contagem WHEA. |
| Slot PCIe, riser ou cabo com defeito | *Minha placa de vídeo registra erros de PCIe?* | Os contadores de erro do sensor da GPU. |

Quais dessas leituras existem depende do seu hardware e do que o HWiNFO suporta
nele. Peça ao assistente para listar os sensores e veja o que o seu PC oferece.

### Vendo o que acontece sob carga

O HWiNFO guarda o mínimo, o máximo e a média de cada leitura desde que foi
aberto (ou desde a última vez que você os zerou na janela de sensores). Isso
transforma duas perguntas em um teste de carga:

1. Com o PC ocioso, peça um panorama térmico.
2. Rode o jogo, a renderização ou o teste de estresse que causa o problema.
3. Pergunte de novo: *Quais foram os picos de temperatura e de consumo da CPU e
   da GPU, e alguma coisa limitou o desempenho?*

### Para ter boas respostas

- Peça o pico, não só o valor atual. Uma CPU que está a 60 °C agora pode ter
  chegado ao limite dez minutos atrás.
- Diga o que estava rodando. Um pico significa pouco sem a carga por trás dele:
  95 °C durante uma renderização é um achado diferente de 95 °C navegando.
- Diga de qual peça você está falando: "GPU", "SSD", "memória", "placa-mãe". O
  assistente então lê menos leituras, e mais relevantes.
- Se o assistente disser que os dados dos sensores não estão disponíveis, peça
  para ele verificar o status do HWiNFO.
- O servidor informa o que o HWiNFO mede. O que o assistente conclui a partir
  dos números é avaliação dele: use como pista a conferir, não como
  diagnóstico.

### Do que uma resposta é feita

Este é um trecho, resumido, do que `thermal_overview` devolveu na máquina de
desenvolvimento. A CPU está a 60 °C agora e chegou a 95,4 °C desde que o HWiNFO
foi aberto. Nenhum indicador de throttling foi acionado, mas `thermal_limit`
chegou a 100 %: naquele pico a CPU estava se mantendo no limite de temperatura.

```json
{
  "active": true,
  "last_update": "2026-11-23T22:55:31-03:00",
  "cpus": [
    {
      "name": "AMD Ryzen 9 9950X3D",
      "temperature": { "label": "CPU (Tctl/Tdie)", "value": 60.125, "unit": "°C", "min": 51.5, "max": 95.375, "avg": 69.557 },
      "package_power": { "label": "CPU Package Power", "value": 98.109, "unit": "W", "min": 55.714, "max": 237.232, "avg": 125.216 },
      "thermal_limit": { "label": "Thermal Limit", "value": 58.913, "unit": "%", "min": 51.351, "max": 100.016, "avg": 69.562 },
      "thermal_throttling": { "active": false, "occurred": false }
    }
  ],
  "gpus": [
    {
      "name": "NVIDIA GeForce RTX 5080",
      "temperature": { "label": "GPU Temperature", "value": 39.813, "unit": "°C", "min": 36.895, "max": 72.27, "avg": 52.341 },
      "hot_spot": { "label": "GPU Hot Spot Temperature", "value": 44.188, "unit": "°C", "min": 41, "max": 94.344, "avg": 60.328 },
      "power": { "label": "GPU Power", "value": 25.117, "unit": "W", "min": 20.717, "max": 319.839, "avg": 115.054 },
      "thermal_throttling": { "active": false, "occurred": false }
    }
  ]
}
```

## Configurações do HWiNFO

As opções do início rápido ficam na aba **General / User Interface** das
configurações do HWiNFO; a tabela diz para que serve cada uma. Depois da
primeira execução, as configurações ficam no menu do ícone do HWiNFO na área de
notificação.

| Opção | Para quê |
|---|---|
| **Shared Memory Support** | Obrigatória. Faz o HWiNFO publicar as leituras para outros programas. |
| **Show Sensors on Startup** | As leituras só são publicadas enquanto os sensores estão rodando. |
| **Minimize Sensors on Startup**, **Minimize Main Window on Startup** | O HWiNFO já abre direto na área de notificação. |
| **Minimize Sensors instead of closing** | Fechar a janela de sensores sem querer não interrompe as leituras. |
| **Auto Start** | O HWiNFO inicia junto com o Windows, então as leituras estão sempre disponíveis. |
| **Show Welcome Screen and Progress**, desativada | O HWiNFO não para na tela inicial toda vez que abre. |

Com isso, basta o HWiNFO estar rodando, com a janela de sensores aberta ou
minimizada na área de notificação.

Os rótulos podem aparecer um pouco diferentes na sua versão do HWiNFO, e são
traduzidos quando ele roda em outro idioma (em português, a janela de sensores
se chama "Estado dos sensores"). O arquivo de configurações é o mesmo em
qualquer caso: `HWiNFO64.INI`, na pasta do HWiNFO (normalmente
`C:\Program Files\HWiNFO64`). Esta é a configuração com que o servidor foi
desenvolvido e testado:

```ini
[Settings]
SensorsOnly=1
SensorsSM=1
ShowWelcomeAndProgress=0
OpenSensors=1
MinimalizeMainWnd=1
MinimalizeSensors=1
MinimalizeSensorsClose=1
Autorun=1
```

`SensorsSM=1` é o Shared Memory Support, a opção que importa.

**O limite de 12 horas.** A edição gratuita do HWiNFO desliga o Shared Memory
Support depois de 12 horas de execução, e então é preciso ativá-lo de novo nas
configurações. O HWiNFO Pro não tem esse limite. Ativar de novo é um passo seu:
este projeto não faz isso por você, e as ferramentas avisam o assistente para
não fazer. Quando as leituras pararem, o assistente sabe dizer o motivo; veja
[Solução de problemas](#solução-de-problemas).

### A cópia do HWiNFO no pacote

O `hwinfo-mcp-with-hwinfo.mcpb` tem uma pasta, `hwinfo`, que o outro pacote não
tem. Nela:

| Arquivo | O que é |
|---|---|
| `hwi_860.zip` | O pacote portátil do HWiNFO, versão 8.60, exatamente como o autor o publica. O SHA-256 dele é `0f7726ee30f6b040cc13aa9de5a19619545a00dd7515fc17ebea9f9f2996e83c`, e cada executável dentro dele tem a assinatura da REALiX. Ele está incluído com a permissão do autor. |
| `HWiNFO64.INI` | Um arquivo de configuração deste projeto: modo somente sensores, Shared Memory Support ligado, sem tela de boas-vindas. |
| `Setup-HWiNFO.cmd` | Um auxiliar de instalação deste projeto. |
| `README.txt` | A mesma explicação, ao lado dos arquivos. |

O HWiNFO não faz parte deste projeto. Ele pertence à REALiX, é gratuito apenas
para uso não comercial e tem [licença própria](https://www.hwinfo.com/licenses/),
que não é a deste projeto.

Nada nessa pasta é executado a menos que você execute. O servidor só diz ao
assistente onde está o auxiliar, e avisa que executá-lo é com você. O auxiliar
é para um PC sem HWiNFO. Executado por você, ele faz três coisas:

1. Descompacta o pacote em `%LOCALAPPDATA%\hwinfo-mcp\HWiNFO`, a menos que o
   HWiNFO já esteja lá. O pacote não é executado de dentro da pasta da
   extensão, porque o Claude Desktop substitui essa pasta a cada atualização.
2. Põe o arquivo de configuração ao lado, a menos que já exista um.
3. Inicia o HWiNFO, a menos que já haja um HWiNFO rodando.

E não mexe no resto. Ele nunca fecha, reinicia nem reconfigura um HWiNFO que
esteja rodando, não faz o HWiNFO iniciar com o Windows, e não tem parte no
limite de 12 horas: quando o HWiNFO desliga o Shared Memory Support, ligá-lo de
novo é feito à mão, como descrito acima. Onde o HWiNFO está instalado no lugar
de costume, `C:\Program Files\HWiNFO64`, o auxiliar avisa e não monta nada;
`Setup-HWiNFO.cmd /portable` monta a cópia dele mesmo assim. No Windows em Arm
ele inicia o `HWiNFO_ARM64.exe` e não acrescenta arquivo de configuração.

Para remover essa cópia do HWiNFO, feche-a e apague a pasta em que ela foi
descompactada. Opções como iniciar com o Windows ficam por sua conta, nas
configurações do HWiNFO.

## Instalando o servidor manualmente

Os pacotes e o plugin já trazem o servidor. Para qualquer outro cliente MCP, ou
para registrar o servidor por conta própria, baixe o executável da
[release mais recente](https://github.com/templarfelix/hwinfo-mcp/releases/latest).
No PowerShell:

```powershell
$dir = "$env:USERPROFILE\tools\hwinfo-mcp"
New-Item -ItemType Directory -Force $dir | Out-Null
Invoke-WebRequest "https://github.com/templarfelix/hwinfo-mcp/releases/latest/download/hwinfo-mcp-windows-amd64.exe" -OutFile "$dir\hwinfo-mcp.exe"
```

No Windows em Arm, use `hwinfo-mcp-windows-arm64.exe`. Para conferir o
download, compare `Get-FileHash "$dir\hwinfo-mcp.exe"` com o arquivo
`SHA256SUMS` da release. O executável não tem assinatura de código, então o
navegador ou o Windows podem exibir um aviso.

Depois, registre o executável no seu cliente.

**Claude Code** (PowerShell):

```powershell
claude mcp add --scope user hwinfo -- "$env:USERPROFILE\tools\hwinfo-mcp\hwinfo-mcp.exe"
claude mcp get hwinfo
```

O segundo comando deve mostrar `Connected`. No `cmd.exe`, escreva o caminho
como `%USERPROFILE%\tools\hwinfo-mcp\hwinfo-mcp.exe`. Abra uma sessão nova
depois; uma que já estava aberta pode ainda não enxergar o servidor.

**Claude Desktop**: adicione o servidor ao `claude_desktop_config.json` e
reinicie o aplicativo.

```json
{
  "mcpServers": {
    "hwinfo": {
      "command": "C:\\Users\\voce\\tools\\hwinfo-mcp\\hwinfo-mcp.exe"
    }
  }
}
```

**Qualquer outro cliente MCP** que consiga iniciar um servidor stdio funciona
do mesmo jeito: o comando é o caminho do executável, sem argumentos.

### Compilando a partir do código-fonte

Em vez de baixar o executável, você pode compilá-lo com Go:

```
go install github.com/templarfelix/hwinfo-mcp@latest
```

Isso coloca o `hwinfo-mcp.exe` na pasta bin do Go, normalmente
`%USERPROFILE%\go\bin`; registre esse caminho como descrito acima.

## Ferramentas

| Ferramenta | O que devolve |
|---|---|
| `hwinfo_status` | Se o HWiNFO está publicando, quando foi a última leitura, quantos sensores e leituras existem e se a cópia foi feita com a trava do HWiNFO. Quando algo está errado, diz o quê e como corrigir. |
| `list_sensors` | Todos os sensores, com a quantidade de leituras de cada tipo. |
| `get_readings` | As leituras atuais agrupadas por sensor, restritas pelos filtros abaixo. |
| `sample_readings` | O que as leituras escolhidas fizeram ao longo de alguns segundos: primeiro e último valor, mínimo, máximo e média, a maior variação entre duas amostras e quando cada extremo aconteceu. |
| `thermal_overview` | As leituras que mostram o quanto o PC esquenta: por CPU, a temperatura de controle (Tctl/Tdie ou do encapsulamento), a média do die, as temperaturas por CCD, o núcleo mais quente, a potência do encapsulamento, quanto do limite de temperatura ela usa e os indicadores de throttling; por GPU, as temperaturas do núcleo, do hot spot e da junção da memória, a potência consumida, as ventoinhas e o indicador de limite térmico; além das demais temperaturas e rotações de ventoinhas. |

As cinco são anotadas como somente leitura. `get_readings` e `sample_readings`
recebem argumentos, todos opcionais:

| Argumento | Ferramenta | Significado |
|---|---|---|
| `sensor` | as duas | Parte do nome de um sensor, sem diferenciar maiúsculas: `CPU`, `RTX 5080`, `S.M.A.R.T.` |
| `label` | as duas | Parte do rótulo de uma leitura, sem diferenciar maiúsculas: `Temperature`, `Tctl`, `Limit` |
| `types` | as duas | Tipos de leitura a incluir, entre `temperature`, `voltage`, `fan`, `current`, `power`, `clock`, `usage`, `other` |
| `stats` | `get_readings` | `true` para incluir mínimo, máximo e média |
| `offset` | `get_readings` | Quantas leituras correspondentes pular; passe o `next_offset` da resposta anterior para continuar |
| `seconds` | `sample_readings` | Por quanto tempo observar, de 1 a 30 segundos; 10 se omitido |
| `values` | `sample_readings` | `true` para receber também cada valor amostrado, para até 16 leituras |

Por exemplo, as temperaturas e as ventoinhas da GPU com as estatísticas:

```json
{ "sensor": "GPU", "types": ["temperature", "fan"], "stats": true }
```

Sobre a saída:

- Um PC tem centenas de leituras (484 na máquina de desenvolvimento), e uma
  chamada de `get_readings` devolve no máximo 300. Quando mais leituras
  correspondem, `truncated` vem marcado, `matched` traz o total e `next_offset`
  diz onde continuar: restrinja os filtros, ou passe esse valor em `offset`
  para ler o restante.
- Indicadores que o HWiNFO mostra como Sim/Não vêm como `1` ou `0`, com a
  unidade `Yes/No`.
- Um `value` vem `null` quando o HWiNFO não tem número para a leitura.

### Acompanhando leituras no tempo

`sample_readings` observa por até 30 segundos e tira uma amostra a cada leitura
do HWiNFO, que acontece de 2 em 2 segundos, a menos que você tenha mudado isso
no HWiNFO. Por exemplo, as tensões e as potências da placa de vídeo durante
meio minuto:

```json
{ "sensor": "GPU", "types": ["voltage", "power"], "seconds": 30, "values": true }
```

Dois limites que vale conhecer:

- Nada mais curto que uma leitura aparece, nem para esta ferramenta nem para o
  próprio HWiNFO. Um pico de tensão de milissegundos não deixa rastro; para
  pegá-lo é preciso um osciloscópio.
- Ela não encontra extremo que o mínimo e o máximo deixariam passar. Esses
  cobrem todas as leituras desde que o HWiNFO foi aberto, então "já passou do
  limite alguma vez?" se responde com `get_readings` e `stats`. O que a
  amostragem acrescenta é o momento: se a tensão caiu bem na hora em que o
  consumo subiu.

### O que o cliente enxerga

O servidor repassa os nomes dos sensores do HWiNFO como eles são. Alguns trazem
mais do que o modelo: o sensor de um disco inclui o número de série, e o sensor
do PresentMon traz o nome do aplicativo que ele está observando. Um assistente
que chama `list_sensors` ou `get_readings` recebe isso, como tudo o mais que lê.

### Nomes e idiomas

O HWiNFO guarda dois nomes para cada sensor e cada leitura: o nome original, em
inglês, e o nome exibido, que segue o idioma do HWiNFO e qualquer renomeação
que você tenha feito. Um HWiNFO em português mostra `GPU Temperature` como
`Temperatura GPU`.

- A saída usa o nome original em inglês como `label` (ou `sensor`) e acrescenta
  `display` quando o nome exibido é diferente.
- Os filtros `sensor` e `label` aceitam qualquer um dos dois nomes.
- `thermal_overview` reconhece as leituras somente pelo nome original, então
  funciona em qualquer idioma do HWiNFO.

## Como ele lê os sensores

A cada chamada, o servidor abre o mapeamento `Global\HWiNFO_SENS_SM2` somente
para leitura, mapeia-o e copia a parte em uso. Durante a cópia ele segura o
mutex do HWiNFO, `Global\HWiNFO_SM2_MUTEX` (esperando por ele no máximo 200 ms),
para que a cópia não saia pela metade; se não conseguir o mutex, copia mesmo
assim e confere se o bloco continua igual logo depois. Tudo é liberado antes de
a chamada retornar. Deslocamentos e tamanhos vêm do cabeçalho do bloco e são
conferidos contra o tamanho do mapeamento antes de qualquer leitura.

## Solução de problemas

Peça ao assistente para verificar o status do HWiNFO: `hwinfo_status` informa a
causa. As mais comuns:

| Problema | Causa |
|---|---|
| memória compartilhada não encontrada | O HWiNFO não está rodando, a janela de sensores está fechada, o Shared Memory Support está desligado ou foi ativado sem reiniciar o HWiNFO. Com o pacote que traz o HWiNFO, a resposta também indica o auxiliar de instalação. |
| `active: false`, valores desatualizados | A janela de sensores foi fechada ou o limite de 12 horas da edição gratuita foi atingido. |
| acesso negado | O HWiNFO e o cliente MCP rodam em níveis de integridade diferentes. |
| o assistente não tem as ferramentas do hwinfo | O cliente não foi reiniciado depois de o plugin ou o servidor ser adicionado, ou o caminho do executável está errado. No Claude Code, `claude mcp list` mostra o servidor e se ele conecta. |

## O que foi verificado

O servidor foi validado com o HWiNFO64 8.60 em um Ryzen 9 9950X3D com uma
GeForce RTX 5080, com o HWiNFO em português, e usado a partir do Claude Code.
Os rótulos dessa máquina ficam guardados como fixture de teste
(`internal/hwinfo/testdata`), e o primeiro padrão de cada campo de
`thermal_overview` é coberto por um teste de regressão feito a partir dela.

O plugin foi instalado no Claude Code 2.1.319 pelo marketplace, a partir de uma
release publicada: o download confere com o checksum, o servidor conecta, e as
ferramentas e as skills são registradas. O pacote foi instalado no Claude
Desktop 6.6395.1 para Windows: o servidor conecta e as cinco ferramentas são
registradas. Ainda não foram observadas uma pergunta respondida a partir de um
chat do Claude Desktop nem as skills executadas por um modelo.

O pacote que traz o HWiNFO foi instalado no Claude Desktop para Windows (pacote
6.28899.6.0) na mesma máquina, a partir de uma compilação desta versão. O
pacote dentro da extensão instalada conferiu com o SHA-256 fixado, e, sem
nenhum HWiNFO rodando, `hwinfo_status` indicou o auxiliar de instalação.
Executado à mão, o auxiliar descompactou o pacote e iniciou essa cópia depois
que o Windows pediu permissão de administrador, e quatro segundos depois a
cópia estava publicando as leituras, sem nenhum passo nas configurações. Duas
ressalvas sobre esse teste: a máquina já tinha o HWiNFO instalado, com as
configurações por usuário do HWiNFO no registro do Windows, então um PC que nunca rodou o
HWiNFO não foi testado; e o auxiliar só aprendeu a não mexer onde há um HWiNFO
instalado depois dele, o que desde então só os testes automatizados
exercitaram. Nada disso foi testado no Windows em Arm.

Os padrões para processadores Intel, modelos Ryzen mais antigos e placas de
vídeo AMD ou Intel seguem a nomenclatura do HWiNFO, mas não foram conferidos
nesse hardware. Se `thermal_overview` deixar passar uma leitura no seu sistema,
`get_readings` ainda a devolve, e os padrões ficam em
`internal/hwinfo/filter.go` (`cpuLabel`, `gpuLabel`).

## Dúvidas e relatos de problemas

Abra uma [issue](https://github.com/templarfelix/hwinfo-mcp/issues). Se
`thermal_overview` escolher a leitura errada ou deixar uma de fora no seu
hardware, a saída de `list_sensors` e a de `get_readings` para o sensor da CPU
ou da GPU é o que basta para corrigir. Remova os números de série antes de
enviar.

## Desenvolvimento

As versões das ferramentas ficam fixadas em `.tool-versions`: o Go, e o Node.js,
que só é necessário para conferir o pacote MCP com a ferramenta oficial `mcpb`.
Com o [asdf](https://asdf-vm.com) e os plugins `golang` e `nodejs`, isto as
instala:

```
asdf install
```

O `go.mod` pede a mesma versão do Go, então uma versão diferente do Go baixa
esse toolchain sozinha, e o CI lê as duas versões do mesmo arquivo.

```
go vet ./...
go test -race ./...
go test -run '^$' -fuzz FuzzParse -fuzztime 30s ./internal/hwinfo
go build -trimpath -ldflags "-s -w" -o hwinfo-mcp.exe .
```

O detector de corridas precisa de um compilador C; em uma máquina Windows sem
um, rode `go test ./...` nela e deixe o `-race` para o CI ou para o Linux.

| Caminho | Conteúdo |
|---|---|
| `main.go` | O servidor MCP e suas cinco ferramentas. |
| `internal/hwinfo/layout.go` | O layout da memória compartilhada e sua decodificação. |
| `internal/hwinfo/snapshot.go` | `Parse`, os tipos do snapshot e a parte portátil do leitor. |
| `internal/hwinfo/read_windows.go` | Abertura, trava e cópia da memória compartilhada. |
| `internal/hwinfo/filter.go` | Os filtros e as heurísticas do panorama térmico. |
| `internal/hwinfo/hwinfotest` | Monta blocos sintéticos de memória compartilhada para os testes. |
| `plugin/` | O plugin do Claude Code: manifesto, configuração do MCP e skills. |
| `mcpb/` | O manifesto e o ícone dos pacotes MCP, e a entrada do MCP Registry. |
| `mcpb/hwinfo.json` | Qual pacote portátil do HWiNFO um dos pacotes traz: versão, tamanho e SHA-256, e de onde baixá-lo. |
| `mcpb/hwinfo/` | Os arquivos deste projeto para a pasta `hwinfo` desse pacote: o arquivo de configuração, o auxiliar de instalação e o aviso. |
| `tools/release` | Compila e empacota todos os arquivos de uma release. |
| `tools/marketplace` | Aponta a entrada do marketplace, e o `mcpb/hwinfo.json`, para uma release publicada. |
| `.claude-plugin/marketplace.json` | A entrada do marketplace, que aponta para o plugin da release mais recente. |

As dependências são a biblioteca padrão do Go, `golang.org/x/sys` e o SDK
oficial de MCP para Go.

### Como lançar uma versão

1. Envie uma tag como `v1.2.3`. O CI roda `go run ./tools/release` e anexa à
   release os executáveis, o plugin, os dois pacotes, o `server.json` e o
   `SHA256SUMS`. O mesmo comando, rodado à mão, gera os mesmos arquivos em
   `dist/`, e é assim que se testa antes da tag:
   `claude --plugin-dir dist/hwinfo-plugin.zip` carrega o plugin por uma sessão.
2. Com a release publicada, rode
   `go run ./tools/marketplace -version v1.2.3` e faça o commit das mudanças. O
   comando aponta o `.claude-plugin/marketplace.json` para o novo
   `hwinfo-plugin.zip`, depois de conferir o download com o `SHA256SUMS` da
   release; é desse commit que os usuários do plugin recebem a atualização. E
   aponta o `mcpb/hwinfo.json` para o pacote da release que traz o HWiNFO,
   depois de conferir que ele contém o pacote fixado.

O pacote portátil do HWiNFO não fica no repositório. O `tools/release` o baixa
do endereço que está em `mcpb/hwinfo.json` para o diretório dado em `-hwinfo`,
e só usa o arquivo cujo tamanho e SHA-256 estão fixados ali. Esse endereço é um
espelho indicado na página de download do HWiNFO, e ele só mantém a versão
atual do HWiNFO; quando o arquivo sair de lá, a ferramenta tira o pacote de
dentro do pacote publicado que esse arquivo indica. Para passar a um HWiNFO
mais novo, baixe o pacote portátil dele, confira que o Windows mostra uma
assinatura válida da REALiX em cada executável, e preencha `version`, `file`,
`size`, `sha256` e `url` nesse arquivo, com `bundle` vazio de novo. O
`mcpb/hwinfo/HWiNFO64.INI` e as seções dos READMEs que citam a versão
acompanham a mudança.

## Licença

MIT. Veja [LICENSE](LICENSE).

Este projeto não tem vínculo com o HWiNFO nem com seus autores, e a licença
dele não cobre o HWiNFO: o HWiNFO pertence à REALiX e é gratuito para uso não
comercial, sob [licença própria](https://www.hwinfo.com/licenses/). Um arquivo
de cada release, o `hwinfo-mcp-with-hwinfo.mcpb`, traz o pacote portátil do
HWiNFO sem modificações, com a permissão do autor. O arquivo de configuração e
o auxiliar de instalação ao lado dele são deste projeto.
