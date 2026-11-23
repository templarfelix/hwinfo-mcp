# hwinfo-mcp

[English](README.md) · [Português (Brasil)](README.pt-BR.md) · **Español**

Pregúntale a un asistente de IA por el hardware de tu PC y deja que lea los
sensores reales.

hwinfo-mcp es un servidor [Model Context Protocol](https://modelcontextprotocol.io)
de solo lectura. Le da a un asistente como Claude acceso a todo lo que
[HWiNFO](https://www.hwinfo.com) monitoriza en un PC con Windows: temperaturas,
voltajes, velocidad de los ventiladores, consumo de energía, frecuencias, uso,
salud de los discos y contadores de errores de hardware.

Eso lo convierte en una herramienta para encontrar problemas de hardware. En
vez de recorrer cientos de filas en HWiNFO, preguntas:

> ¿Se está sobrecalentando mi CPU? Revisa la temperatura actual, el pico y si
> llegó a limitar el rendimiento.

El asistente lee los sensores, compara los valores actuales con los picos que
HWiNFO ha registrado y te dice lo que encuentra.

Es un único ejecutable que habla MCP por stdin/stdout. Lee el bloque de memoria
que HWiNFO comparte con otros programas y nunca escribe en él, no abre ninguna
conexión de red y no cambia nada en el sistema.

**Solo Windows.** HWiNFO existe únicamente para Windows, igual que la memoria
compartida que lee este servidor, así que las versiones publicadas son para
Windows en x64 y en Arm. El Linux de WSL 2 tampoco ve los chips de sensores del
PC: ejecuta el servidor del lado de Windows.

## Inicio rápido

### 1. Configura HWiNFO

Quien mide es HWiNFO; este servidor solo lee lo que HWiNFO publica. HWiNFO es
un programa aparte, con su propia licencia, y no viene incluido aquí. Su
publicación viene desactivada, así que hay que activarla una vez.

1. Instala [HWiNFO64](https://www.hwinfo.com/download/), o ejecuta
   `winget install REALiX.HWiNFO`, y ábrelo. Pide permisos de administrador,
   que necesita para acceder al hardware.
2. En la pantalla inicial, marca **Sensors-only** (solo sensores) y haz clic en
   **Settings** (configuración). Más adelante, la misma configuración está en
   el menú del icono de HWiNFO en el área de notificación.
3. En la pestaña **General / User Interface**, activa estas opciones:

   | Opción | Para qué |
   |---|---|
   | **Shared Memory Support** | Obligatoria. Hace que HWiNFO publique sus lecturas para otros programas. |
   | **Show Sensors on Startup** | Las lecturas solo se publican mientras los sensores están en marcha. |
   | **Minimize Sensors on Startup**, **Minimize Main Window on Startup** | HWiNFO arranca directamente en el área de notificación. |
   | **Minimize Sensors instead of closing** | Cerrar la ventana de sensores por accidente no detiene las lecturas. |
   | **Auto Start** | HWiNFO arranca con Windows, así que las lecturas siempre están disponibles. |

   Y desactiva **Show Welcome Screen and Progress**, para que HWiNFO no se
   detenga en la pantalla inicial cada vez que arranca.

4. Confirma con **OK** y reinicia HWiNFO: ciérralo desde su icono del área de
   notificación y ábrelo de nuevo. En la máquina donde se desarrolló este
   proyecto, las lecturas solo empezaron a publicarse después de ese reinicio.

A partir de ahí basta con que HWiNFO esté en ejecución, con la ventana de
sensores abierta o minimizada en el área de notificación.

Las etiquetas pueden verse algo distintas en tu versión de HWiNFO, y se
traducen cuando HWiNFO se ejecuta en otro idioma. El archivo de configuración
es el mismo en cualquier caso: `HWiNFO64.INI`, en la carpeta de HWiNFO
(normalmente `C:\Program Files\HWiNFO64`). Esta es la configuración con la que
se desarrolló y probó el servidor:

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

`SensorsSM=1` es Shared Memory Support, la opción que importa.

**El límite de 12 horas.** La edición gratuita de HWiNFO desactiva Shared
Memory Support tras 12 horas de ejecución, y entonces hay que volver a
activarlo en la configuración. HWiNFO Pro no tiene ese límite. Cuando las
lecturas se detengan, el asistente puede decirte por qué; consulta
[Solución de problemas](#solución-de-problemas).

### 2. Instálalo

Elige el camino según tu cliente.

#### Claude Code: el plugin

El plugin trae el servidor junto con tres
[skills](#skills-que-vienen-con-el-plugin) que le dan al asistente un método
para leer los sensores. Desde una terminal:

```
claude plugin marketplace add templarfelix/hwinfo-mcp
claude plugin install hwinfo@hwinfo-mcp
```

O dentro de una sesión de Claude Code, con la versión 2.1.275 o posterior:

```
/plugin install hwinfo --marketplace templarfelix/hwinfo-mcp
```

El plugin trae su propia copia del servidor, así que no hay ningún ejecutable
que descargar.

#### Claude Desktop: el paquete

Descarga
[`hwinfo-mcp.mcpb`](https://github.com/templarfelix/hwinfo-mcp/releases/latest/download/hwinfo-mcp.mcpb)
y ábrelo, o arrástralo a Claude Desktop. El paquete es nuevo en la v0.2.0 y
hasta ahora solo se ha comprobado con la propia herramienta de empaquetado. Si
no se instala, usa el ejecutable como se describe a continuación y, por favor,
abre una issue.

#### Cualquier cliente MCP: el ejecutable

Descarga el ejecutable de la
[última versión](https://github.com/templarfelix/hwinfo-mcp/releases/latest).
En PowerShell:

```powershell
$dir = "$env:USERPROFILE\tools\hwinfo-mcp"
New-Item -ItemType Directory -Force $dir | Out-Null
Invoke-WebRequest "https://github.com/templarfelix/hwinfo-mcp/releases/latest/download/hwinfo-mcp-windows-amd64.exe" -OutFile "$dir\hwinfo-mcp.exe"
```

En Windows sobre Arm, usa `hwinfo-mcp-windows-arm64.exe`. Para comprobar la
descarga, compara `Get-FileHash "$dir\hwinfo-mcp.exe"` con el archivo
`SHA256SUMS` de la versión. El ejecutable no tiene firma de código, por lo que
el navegador o Windows pueden mostrar una advertencia.

O compílalo tú mismo con Go:

```
go install github.com/templarfelix/hwinfo-mcp@latest
```

Eso deja `hwinfo-mcp.exe` en la carpeta bin de Go, normalmente
`%USERPROFILE%\go\bin`; usa esa ruta más abajo.

Después, registra el ejecutable en tu cliente.

**Claude Code** (PowerShell):

```powershell
claude mcp add --scope user hwinfo -- "$env:USERPROFILE\tools\hwinfo-mcp\hwinfo-mcp.exe"
claude mcp get hwinfo
```

El segundo comando debe mostrar `Connected`. En `cmd.exe`, escribe la ruta como
`%USERPROFILE%\tools\hwinfo-mcp\hwinfo-mcp.exe`. Abre una sesión nueva después;
una que ya estaba abierta puede no ver todavía el servidor.

**Claude Desktop**: añade el servidor a `claude_desktop_config.json` y reinicia
la aplicación.

```json
{
  "mcpServers": {
    "hwinfo": {
      "command": "C:\\Users\\tu-usuario\\tools\\hwinfo-mcp\\hwinfo-mcp.exe"
    }
  }
}
```

**Cualquier otro cliente MCP** capaz de iniciar un servidor stdio funciona
igual: el comando es la ruta del ejecutable, sin argumentos.

### 3. Comprueba que funciona

Pregúntale al asistente:

> ¿HWiNFO está publicando los datos de los sensores?

Llama a `hwinfo_status` y debería responder que los sensores están activos y de
cuándo es la última lectura. Si no lo están, la respuesta dice qué falla y cómo
arreglarlo.

## Cómo preguntar

No llamas a las herramientas directamente. Pregunta con tus palabras, en
cualquier idioma; el asistente elige la herramienta y los filtros. Algunos
ejemplos para empezar:

- ¿A qué temperatura están mi CPU y mi GPU ahora mismo?
- ¿A qué velocidad giran los ventiladores?
- ¿Cuánta energía consumen el procesador y la tarjeta gráfica?
- ¿Qué sensores ve HWiNFO en este PC?

### Skills que vienen con el plugin

El plugin añade tres skills. El asistente las usa por su cuenta cuando la
pregunta lo pide, y también puedes iniciar una por su nombre:

| Skill | Qué hace |
|---|---|
| `/hwinfo:thermal-check` | Evalúa si la CPU o la tarjeta gráfica se calientan demasiado, si la temperatura las está frenando o si necesitan pasta térmica o thermal pads nuevos. Trabaja con temperatura y potencia leídas en el mismo instante, bajo carga, y dice lo que los sensores no pueden mostrar. |
| `/hwinfo:hardware-checkup` | Recorre los contadores de errores de hardware, la salud de los discos, los voltajes, los ventiladores, la memoria y los límites, e informa de lo que requiere atención, lo que está bien y lo que no se pudo comprobar. |
| `/hwinfo:setup` | Consigue que HWiNFO publique sus lecturas, o explica por qué se detuvieron. |

Sin el plugin las mismas preguntas funcionan; el asistente tiene entonces las
herramientas, pero no el método.

### A la caza de problemas de hardware

| Sospechas que | Pregunta algo como | Qué lee el asistente |
|---|---|---|
| La CPU se sobrecalienta | *¿Se está sobrecalentando mi CPU? Compara la temperatura actual con el pico y dime si hubo throttling.* | La temperatura de control de la CPU (Tctl/Tdie o del encapsulado) con mínimo, máximo y media, las temperaturas por CCD y del núcleo más caliente, la potencia del encapsulado y los indicadores de throttling térmico. |
| El rendimiento cae bajo carga | *¿Algo está frenando mi CPU o mi GPU? Mira los límites térmicos, de potencia y de corriente.* | Lecturas como `Thermal Limit`, `CPU PPT Limit`, `Thermal Throttling (HTC)` y, en la GPU, `Performance Limit - Thermal` y `Performance Limit - Power`. |
| La tarjeta gráfica se calienta demasiado, o la pasta y los pads térmicos están gastados | *Compara el hot spot de la GPU con la temperatura del núcleo y revisa la temperatura de la memoria.* | Temperaturas del núcleo, del hot spot y de la unión de la memoria de la GPU, la potencia de la tarjeta y la velocidad de los ventiladores. |
| Un ventilador ha muerto o se atasca | *¿Giran todos los ventiladores? Muestra la velocidad actual, la mínima y la máxima de cada uno.* | Todas las lecturas de ventilador, con sus estadísticas. |
| El PC se apaga o se reinicia bajo carga | *Mi PC se reinicia cuando juego. Revisa las temperaturas, las líneas de 12 V, 5 V y 3,3 V y los contadores de errores de hardware.* | Temperaturas, los voltajes de la placa base con mínimo y máximo, y el recuento de errores de hardware de Windows (WHEA). |
| Un disco está fallando | *¿Cómo está la salud de mi SSD?* | El sensor S.M.A.R.T.: vida útil restante, reserva disponible, indicadores de fallo y de aviso, temperatura. |
| Memoria u overclock inestable | *¿Hay algún indicio de problemas de memoria? Revisa la temperatura y los indicadores de los módulos y los errores de hardware.* | Los sensores de los módulos de memoria (temperatura, voltajes, indicadores del PMIC) y el recuento WHEA. |
| Una ranura PCIe, un riser o un cable defectuoso | *¿Mi tarjeta gráfica registra errores de PCIe?* | Los contadores de errores del sensor de la GPU. |

Cuáles de estas lecturas existen depende de tu hardware y de lo que HWiNFO
soporte en él. Pídele al asistente que liste los sensores para ver qué ofrece
tu PC.

### Ver qué ocurre bajo carga

HWiNFO guarda el mínimo, el máximo y la media de cada lectura desde que se
abrió (o desde la última vez que los restableciste en la ventana de sensores).
Eso convierte dos preguntas en una prueba de carga:

1. Con el PC en reposo, pide un resumen térmico.
2. Ejecuta el juego, el render o la prueba de estrés que causa el problema.
3. Pregunta de nuevo: *¿Cuáles fueron los picos de temperatura y de consumo de
   la CPU y la GPU, y algo limitó el rendimiento?*

### Para obtener buenas respuestas

- Pide el pico, no solo el valor actual. Una CPU que ahora está a 60 °C puede
  haber llegado a su límite hace diez minutos.
- Di qué se estaba ejecutando. Un pico significa poco sin la carga que lo
  causó: 95 °C durante un render es un hallazgo distinto de 95 °C navegando.
- Nombra la pieza a la que te refieres: "GPU", "SSD", "memoria", "placa base".
  El asistente lee entonces menos lecturas, y más relevantes.
- Si el asistente dice que los datos de los sensores no están disponibles,
  pídele que revise el estado de HWiNFO.
- El servidor informa de lo que HWiNFO mide. Lo que el asistente concluye a
  partir de los números es su propio juicio: úsalo como pista que comprobar, no
  como diagnóstico.

### De qué está hecha una respuesta

Esto es parte, abreviada, de lo que `thermal_overview` devolvió en la máquina
de desarrollo. La CPU está a 61,75 °C ahora, llegó a 95,4 °C desde que se abrió
HWiNFO, y no se activó ningún indicador de throttling:

```json
{
  "active": true,
  "last_update": "2026-11-23T19:01:38-03:00",
  "cpus": [
    {
      "name": "AMD Ryzen 9 9950X3D",
      "temperature": { "label": "CPU (Tctl/Tdie)", "value": 61.75, "unit": "°C", "min": 51.5, "max": 95.375, "avg": 70.259 },
      "package_power": { "label": "CPU Package Power", "value": 84.284, "unit": "W", "min": 55.714, "max": 237.232, "avg": 129.425 },
      "thermal_throttling": { "active": false, "occurred": false }
    }
  ],
  "gpus": [
    {
      "name": "NVIDIA GeForce RTX 5080",
      "temperature": { "label": "GPU Temperature", "value": 48.434, "unit": "°C", "min": 37.316, "max": 67.414, "avg": 51.215 },
      "hot_spot": { "label": "GPU Hot Spot Temperature", "value": 53.531, "unit": "°C", "min": 41.5, "max": 86.031, "avg": 59.193 },
      "thermal_throttling": { "active": false, "occurred": false }
    }
  ]
}
```

## Herramientas

| Herramienta | Qué devuelve |
|---|---|
| `hwinfo_status` | Si HWiNFO está publicando, cuándo fue la última lectura, cuántos sensores y lecturas hay y si la copia se tomó con el bloqueo de HWiNFO. Cuando algo va mal, dice qué y cómo arreglarlo. |
| `list_sensors` | Todos los sensores, con el número de lecturas de cada tipo. |
| `get_readings` | Las lecturas actuales agrupadas por sensor, acotadas por los filtros de abajo. |
| `thermal_overview` | Las lecturas que muestran cuánto se calienta el PC: por CPU, la temperatura de control (Tctl/Tdie o del encapsulado), la media del die, las temperaturas por CCD, el núcleo más caliente, la potencia del encapsulado y los indicadores de throttling; por GPU, las temperaturas del núcleo, del hot spot y de la unión de la memoria, la potencia de la tarjeta, los ventiladores y el indicador de límite térmico; además del resto de temperaturas y velocidades de ventilador. |

Las cuatro están anotadas como de solo lectura, y solo `get_readings` recibe
argumentos, todos opcionales:

| Argumento | Significado |
|---|---|
| `sensor` | Parte del nombre de un sensor, sin distinguir mayúsculas: `CPU`, `RTX 5080`, `S.M.A.R.T.` |
| `label` | Parte de la etiqueta de una lectura, sin distinguir mayúsculas: `Temperature`, `Tctl`, `Limit` |
| `types` | Tipos de lectura que incluir, entre `temperature`, `voltage`, `fan`, `current`, `power`, `clock`, `usage`, `other` |
| `stats` | `true` para añadir mínimo, máximo y media |

Por ejemplo, las temperaturas y los ventiladores de la GPU con sus
estadísticas:

```json
{ "sensor": "GPU", "types": ["temperature", "fan"], "stats": true }
```

Sobre la salida:

- Un PC tiene cientos de lecturas (484 en la máquina de desarrollo), y una
  llamada a `get_readings` devuelve como máximo 300. Cuando coinciden más,
  `truncated` viene marcado y `matched` da el total; acota los filtros para ver
  el resto.
- Los indicadores que HWiNFO muestra como Sí/No llegan como `1` o `0`, con la
  unidad `Yes/No`.
- Un `value` es `null` cuando HWiNFO no tiene un número para la lectura.

### Lo que ve el cliente

El servidor transmite los nombres de los sensores de HWiNFO tal como son.
Algunos llevan más que el modelo: el sensor de un disco incluye su número de
serie, y el sensor de PresentMon nombra la aplicación que está observando. Un
asistente que llama a `list_sensors` o a `get_readings` recibe eso, como todo
lo demás que lee.

### Nombres e idiomas

HWiNFO guarda dos nombres para cada sensor y cada lectura: el original, en
inglés, y el que muestra, que sigue el idioma de HWiNFO y cualquier cambio de
nombre que hayas hecho. Un HWiNFO en portugués muestra `GPU Temperature` como
`Temperatura GPU`.

- La salida usa el nombre original en inglés como `label` (o `sensor`) y añade
  `display` cuando el nombre mostrado es distinto.
- Los filtros `sensor` y `label` aceptan cualquiera de los dos nombres.
- `thermal_overview` reconoce las lecturas solo por su nombre original, así que
  funciona sea cual sea el idioma de HWiNFO.

## Cómo lee los sensores

En cada llamada, el servidor abre el mapeo `Global\HWiNFO_SENS_SM2` en solo
lectura, lo mapea y copia la parte en uso. Mientras copia retiene el mutex de
HWiNFO, `Global\HWiNFO_SM2_MUTEX` (esperándolo como máximo 200 ms), para que la
copia no quede a medias; si no consigue el mutex, copia de todos modos y
comprueba que el bloque siga leyéndose igual justo después. Todo se libera
antes de que la llamada termine. Los desplazamientos y tamaños salen de la
cabecera del bloque y se comprueban contra el tamaño del mapeo antes de leer
nada.

## Solución de problemas

Pídele al asistente que revise el estado de HWiNFO: `hwinfo_status` indica la
causa. Las habituales:

| Problema | Causa |
|---|---|
| memoria compartida no encontrada | HWiNFO no está en ejecución, la ventana de sensores está cerrada, Shared Memory Support está desactivado o se activó sin reiniciar HWiNFO. |
| `active: false`, valores obsoletos | Se cerró la ventana de sensores o se alcanzó el límite de 12 horas de la edición gratuita. |
| acceso denegado | HWiNFO y el cliente MCP se ejecutan con niveles de integridad distintos. |
| el asistente no tiene las herramientas de hwinfo | El cliente no se reinició tras añadir el servidor, o la ruta del ejecutable es incorrecta. En Claude Code, `claude mcp get hwinfo` muestra la ruta y si conecta. |

## Qué se ha verificado

El servidor se validó con HWiNFO64 8.60 en un Ryzen 9 9950X3D con una GeForce
RTX 5080, con HWiNFO en portugués, y se usó desde Claude Code. Las etiquetas de
esa máquina se conservan como fixture de prueba (`internal/hwinfo/testdata`), y
el primer patrón de cada campo de `thermal_overview` está cubierto por una
prueba de regresión construida a partir de ella.

El plugin se instaló en Claude Code 2.1.319 a través del marketplace, desde la
versión publicada: la descarga coincide con su checksum, el servidor conecta, y
las herramientas y las skills se registran. El paquete MCP se validó y
empaquetó con la herramienta oficial `mcpb`, pero aún no se había instalado en
Claude Desktop cuando se publicó la v0.2.0.

Los patrones para procesadores Intel, modelos Ryzen más antiguos y tarjetas
gráficas AMD o Intel siguen la nomenclatura de HWiNFO, pero no se han
comprobado en ese hardware. Si `thermal_overview` pasa por alto una lectura en
tu sistema, `get_readings` la devuelve igualmente, y los patrones están en
`internal/hwinfo/filter.go` (`cpuLabel`, `gpuLabel`).

## Dudas e informes de errores

Abre una [issue](https://github.com/templarfelix/hwinfo-mcp/issues). Si
`thermal_overview` elige la lectura equivocada o deja una fuera en tu hardware,
la salida de `list_sensors` y la de `get_readings` para el sensor de la CPU o
de la GPU es lo que hace falta para corregirlo. Quita antes los números de
serie.

## Desarrollo

Las versiones de las herramientas están fijadas en `.tool-versions`: Go, y
Node.js para empaquetar el paquete MCP. Con [asdf](https://asdf-vm.com) y sus
plugins `golang` y `nodejs`, esto las instala:

```
asdf install
```

`go.mod` pide la misma versión de Go, así que una versión distinta de Go
descarga ese toolchain por su cuenta, y el CI lee ambas versiones del mismo
archivo.

```
go vet ./...
go test -race ./...
go test -run '^$' -fuzz FuzzParse -fuzztime 30s ./internal/hwinfo
go build -trimpath -ldflags "-s -w" -o hwinfo-mcp.exe .
```

El detector de carreras necesita un compilador de C; en una máquina Windows sin
uno, ejecuta `go test ./...` allí y deja `-race` para el CI o para Linux.

| Ruta | Contenido |
|---|---|
| `main.go` | El servidor MCP y sus cuatro herramientas. |
| `internal/hwinfo/layout.go` | El formato de la memoria compartida y su decodificación. |
| `internal/hwinfo/snapshot.go` | `Parse`, los tipos del snapshot y la parte portable del lector. |
| `internal/hwinfo/read_windows.go` | Apertura, bloqueo y copia de la memoria compartida. |
| `internal/hwinfo/filter.go` | Los filtros y las heurísticas del resumen térmico. |
| `internal/hwinfo/hwinfotest` | Construye bloques sintéticos de memoria compartida para las pruebas. |
| `plugin/` | El plugin de Claude Code: manifiesto, configuración de MCP y skills. |
| `mcpb/` | El manifiesto del paquete MCP y la entrada del MCP Registry. |
| `tools/release` | Compila y empaqueta todos los archivos de una versión. |
| `.claude-plugin/marketplace.json` | La entrada del marketplace, que apunta al plugin de la última versión. |

Las dependencias son la biblioteca estándar de Go, `golang.org/x/sys` y el SDK
oficial de MCP para Go.

### Cómo publicar una versión

1. Envía una etiqueta como `v1.2.3`. El CI ejecuta `go run ./tools/release` y
   adjunta a la versión los ejecutables, el plugin, el paquete, `server.json` y
   `SHA256SUMS`. El mismo comando, ejecutado a mano, genera los mismos archivos
   en `dist/`, y así es como se prueban antes de etiquetar:
   `claude --plugin-dir dist/hwinfo-plugin.zip` carga el plugin durante una
   sesión.
2. Con la versión publicada, apunta `.claude-plugin/marketplace.json` al nuevo
   `hwinfo-plugin.zip`: su URL y su SHA-256, que está en el `SHA256SUMS` de la
   versión. De ese commit reciben la actualización los usuarios del plugin.

## Licencia

MIT. Consulta [LICENSE](LICENSE).

Este proyecto no está afiliado a HWiNFO ni a sus autores.
